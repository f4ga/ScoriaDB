// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/f4ga/ScoriaDB/internal/logger"
)

const (
	// manifestMagic is a fixed 4-byte identifier for the manifest file format.
	manifestMagic = 0x53434F52 // "SCOR" in little-endian
	// manifestVersion is the current version of the manifest format.
	manifestVersion = 1
	// manifestDefaultLevels is the number of level slots pre-allocated in a
	// fresh manifest so consumers indexing by level never panic.
	manifestDefaultLevels = 10
)

// Manifest manages the persistent metadata (SSTable levels, file numbers, etc.).
// It uses a length-prefixed JSON format for crash-safety and recovery.
type Manifest struct {
	mu   sync.Mutex
	file *os.File
	// cache of the current state (levels, next file number, etc.)
	levels      [][]SSTableInfo
	nextFileNum uint64
	lastTS      uint64
}

// SSTableInfo describes an SSTable file in the manifest.
type SSTableInfo struct {
	FileNum uint64
	Level   int
	MinKey  []byte
	MaxKey  []byte
	Size    uint64
}

// VersionEdit represents a change to the manifest state.
type VersionEdit struct {
	NewFiles     []SSTableInfo
	DeletedFiles []struct {
		FileNum uint64
		Level   int
	}
	NextFileNum uint64
	LastTS      uint64
}

// NewManifest opens or creates a manifest file.
func NewManifest(path string) (*Manifest, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}

	m := &Manifest{
		file: file,
	}
	if err := m.recover(); err != nil {
		file.Close()
		return nil, fmt.Errorf("recover manifest: %w", err)
	}
	return m, nil
}

// Apply applies a VersionEdit atomically (writes to file and updates state).
func (m *Manifest) Apply(edit *VersionEdit) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Apply to in-memory state first (idempotent)
	m.applyEdit(edit)

	// Write the edit to disk with length prefix.
	if err := m.writeEdit(edit); err != nil {
		// Rollback in-memory state? For simplicity, we log error and return.
		// In production, you might want to revert or panic.
		logger.Warn("failed to write manifest edit: %v", err)
		return err
	}
	return nil
}

// applyEdit updates the in-memory state from a VersionEdit.
func (m *Manifest) applyEdit(edit *VersionEdit) {
	// Apply deletions (in reverse order to avoid index issues)
	for _, del := range edit.DeletedFiles {
		m.deleteFile(del.FileNum, del.Level)
	}
	// Add new files
	for _, info := range edit.NewFiles {
		m.addFile(info)
	}
	if edit.NextFileNum > 0 {
		m.nextFileNum = edit.NextFileNum
	}
	if edit.LastTS > m.lastTS {
		m.lastTS = edit.LastTS
	}
}

func (m *Manifest) addFile(info SSTableInfo) {
	if info.Level >= len(m.levels) {
		newLevels := make([][]SSTableInfo, info.Level+1)
		copy(newLevels, m.levels)
		m.levels = newLevels
	}
	m.levels[info.Level] = append(m.levels[info.Level], info)
}

func (m *Manifest) deleteFile(fileNum uint64, level int) {
	if level >= len(m.levels) {
		return
	}
	levelSlice := m.levels[level]
	for i, info := range levelSlice {
		if info.FileNum == fileNum {
			m.levels[level] = append(levelSlice[:i], levelSlice[i+1:]...)
			break
		}
	}
}

// writeEdit writes a VersionEdit to the file with a length prefix.
// It assumes m.mu is held.
//
// On a fresh file (size 0) a magic+version header is written first so recover()
// can unambiguously detect the length-prefixed format. Legacy JSON-only files
// (no header) are still recovered via recoverLegacy().
func (m *Manifest) writeEdit(edit *VersionEdit) error {
	// If the file is empty, write the format header first so recovery can
	// distinguish the length-prefixed format from the legacy JSON-only format.
	stat, err := m.file.Stat()
	if err != nil {
		return fmt.Errorf("stat manifest: %w", err)
	}
	if stat.Size() == 0 {
		header := make([]byte, 8)
		binary.LittleEndian.PutUint32(header[0:4], manifestMagic)
		binary.LittleEndian.PutUint32(header[4:8], manifestVersion)
		if _, err := m.file.Write(header); err != nil {
			return fmt.Errorf("write manifest header: %w", err)
		}
	}

	data, err := json.Marshal(edit)
	if err != nil {
		return fmt.Errorf("marshal edit: %w", err)
	}

	// Write length prefix (4 bytes, little-endian)
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(data)))
	if _, err := m.file.Write(lenBuf); err != nil {
		return fmt.Errorf("write length: %w", err)
	}
	if _, err := m.file.Write(data); err != nil {
		return fmt.Errorf("write data: %w", err)
	}
	// Ensure durability
	if err := m.file.Sync(); err != nil {
		return fmt.Errorf("sync manifest: %w", err)
	}
	return nil
}

// recover reads the manifest file and rebuilds the in-memory state.
// It handles both the new length-prefixed format and the legacy JSON-only format.
//
// On a fresh/empty file, levels must still be initialized to a fixed number of
// slots (so GetLevels/applyEdit never index out of range). A partial trailing
// record (crash mid-write) is ignored, but corruption with valid data following
// it is reported as an error rather than silently dropped.
func (m *Manifest) recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Reset in-memory state. levels keeps a fixed slot count so consumers that
	// index by level never panic; addFile grows it when a deeper level appears.
	m.levels = make([][]SSTableInfo, manifestDefaultLevels)
	m.nextFileNum = 0
	m.lastTS = 0

	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek: %w", err)
	}

	// Try to read the magic number to detect format.
	var magic uint32
	err := binary.Read(m.file, binary.LittleEndian, &magic)
	if err != nil {
		// If we can't read magic, the file might be empty or old format.
		if err == io.EOF {
			// Empty file – treat as new: first file number is 1.
			if m.nextFileNum == 0 {
				m.nextFileNum = 1
			}
			return nil
		}
		// Not EOF: fall back to legacy recovery.
		return m.recoverLegacy()
	}

	if magic != manifestMagic {
		// Not our magic – assume legacy format.
		return m.recoverLegacy()
	}

	// Read version.
	var version uint32
	if err := binary.Read(m.file, binary.LittleEndian, &version); err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	if version != manifestVersion {
		// For future versions, we could implement migration.
		return fmt.Errorf("unsupported manifest version: %d", version)
	}

	// Read length-prefixed records.
	for {
		var length uint32
		if err := binary.Read(m.file, binary.LittleEndian, &length); err != nil {
			if err == io.EOF {
				break // End of file – normal.
			}
			// If we got an error reading the length, check if there is trailing partial data.
			remaining, readErr := io.ReadAll(m.file)
			if readErr != nil {
				return fmt.Errorf("failed to read trailing manifest data: %w", readErr)
			}
			if len(remaining) > 0 {
				return fmt.Errorf("corrupted manifest: failed to read length with %d bytes remaining", len(remaining))
			}
			break // No remaining data, likely a partial last record – ignore.
		}

		data := make([]byte, length)
		if _, err := io.ReadFull(m.file, data); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// Partial record at the end – ignore it.
				break
			}
			return fmt.Errorf("read record data: %w", err)
		}

		var edit VersionEdit
		if err := json.Unmarshal(data, &edit); err != nil {
			// A fully-read record that fails to parse is either a partial write of
			// the LAST record (crash mid-write) or corruption in the MIDDLE of the
			// file. Distinguish by whether valid data follows:
			//   - no data after -> partial last record -> skip it (keep prior state)
			//   - data after     -> mid-file corruption -> report error so fresh
			//     SSTables are not silently lost
			remaining, readErr := io.ReadAll(m.file)
			if readErr != nil {
				return fmt.Errorf("failed to read trailing manifest data: %w", readErr)
			}
			if len(remaining) > 0 {
				return fmt.Errorf("corrupted manifest: invalid JSON with %d bytes remaining", len(remaining))
			}
			// Partial/corrupted last record – ignore it, preserving earlier records.
			break
		}
		m.applyEdit(&edit)
	}

	if m.nextFileNum == 0 {
		m.nextFileNum = 1
	}
	return nil
}

// recoverLegacy reads the manifest using the old JSON-only format (no magic, no length prefix).
// This is used for backward compatibility.
func (m *Manifest) recoverLegacy() error {
	// Reset state. levels keeps a fixed slot count so consumers indexing by
	// level never panic; addFile grows it when a deeper level appears.
	m.levels = make([][]SSTableInfo, manifestDefaultLevels)
	m.nextFileNum = 0
	m.lastTS = 0

	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("legacy seek: %w", err)
	}

	decoder := json.NewDecoder(m.file)
	for {
		var edit VersionEdit
		if err := decoder.Decode(&edit); err != nil {
			if err == io.EOF {
				break
			}
			// In legacy mode we cannot distinguish corruption from truncation.
			// We break but do not return an error to avoid losing previous entries.
			break
		}
		m.applyEdit(&edit)
	}
	if m.nextFileNum == 0 {
		m.nextFileNum = 1
	}
	return nil
}

// Close closes the manifest file.
func (m *Manifest) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.file.Close()
}

// GetLevels returns a copy of the current levels.
func (m *Manifest) GetLevels() [][]SSTableInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([][]SSTableInfo, len(m.levels))
	for i, level := range m.levels {
		result[i] = append([]SSTableInfo(nil), level...)
	}
	return result
}

// NextFileNum returns the next available file number and increments it.
func (m *Manifest) NextFileNum() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	num := m.nextFileNum
	m.nextFileNum++
	return num
}

// LastTS returns the last stored timestamp.
func (m *Manifest) LastTS() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastTS
}

// SetLastTS updates the stored timestamp.
func (m *Manifest) SetLastTS(ts uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ts > m.lastTS {
		m.lastTS = ts
	}
}
