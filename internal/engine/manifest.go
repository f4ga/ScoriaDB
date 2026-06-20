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
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/keys"
)

// SSTableInfo contains metadata for a single SSTable file.
type SSTableInfo struct {
	FileNum uint64 `json:"file_num"`
	Level   int    `json:"level"`
	MinKey  []byte `json:"min_key"`
	MaxKey  []byte `json:"max_key"`
	Size    uint64 `json:"size"`
}

// VersionEdit represents an atomic change to the file set.
type VersionEdit struct {
	NewFiles     []SSTableInfo `json:"new_files,omitempty"`
	DeletedFiles []SSTableInfo `json:"deleted_files,omitempty"`
	NextFileNum  uint64        `json:"next_file_num,omitempty"`
}

// Manifest manages the SSTable metadata log.
type Manifest struct {
	mu          sync.Mutex
	vfs         vfs.VFS
	file        vfs.File
	filePath    string
	levels      [][]SSTableInfo
	nextFileNum uint64
}

// NewManifest creates or opens a manifest at the given path.
func NewManifest(vfs vfs.VFS, path string) (*Manifest, error) {
	if err := vfs.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create manifest directory: %w", err)
	}

	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest file: %w", err)
	}

	m := &Manifest{
		vfs:         vfs,
		file:        file,
		filePath:    path,
		levels:      make([][]SSTableInfo, 10),
		nextFileNum: 1,
	}

	if err := m.recover(); err != nil {
		errors.CloseWithLog(file, "manifest-file")
		return nil, fmt.Errorf("failed to recover manifest: %w", err)
	}

	return m, nil
}

// recover reads all entries from the file and applies them.
func (m *Manifest) recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek failed: %w", err)
	}

	decoder := json.NewDecoder(m.file)
	for {
		var edit VersionEdit
		if err := decoder.Decode(&edit); err != nil {
			if err == io.EOF {
				break
			}
			break
		}
		m.applyEdit(&edit)
	}

	return nil
}

// applyEdit applies a VersionEdit to the in-memory state.
func (m *Manifest) applyEdit(edit *VersionEdit) {
	for _, df := range edit.DeletedFiles {
		if df.Level < len(m.levels) {
			filtered := make([]SSTableInfo, 0, len(m.levels[df.Level]))
			for _, f := range m.levels[df.Level] {
				if f.FileNum != df.FileNum {
					filtered = append(filtered, f)
				}
			}
			m.levels[df.Level] = filtered
		}
	}

	for _, nf := range edit.NewFiles {
		level := nf.Level
		if level >= len(m.levels) {
			newLevels := make([][]SSTableInfo, level+1)
			copy(newLevels, m.levels)
			m.levels = newLevels
		}
		m.levels[level] = append(m.levels[level], nf)
		sort.Slice(m.levels[level], func(i, j int) bool {
			return keys.CompareKeys(m.levels[level][i].MinKey, m.levels[level][j].MinKey) < 0
		})
	}

	if edit.NextFileNum > 0 {
		m.nextFileNum = edit.NextFileNum
	}
}

// Apply writes a VersionEdit to the manifest and applies it.
func (m *Manifest) Apply(edit *VersionEdit) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.Marshal(edit)
	if err != nil {
		return fmt.Errorf("failed to marshal version edit: %w", err)
	}
	data = append(data, '\n')

	if _, err := m.file.Write(data); err != nil {
		return fmt.Errorf("failed to write manifest entry: %w", err)
	}
	if err := m.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync manifest: %w", err)
	}

	m.applyEdit(edit)
	return nil
}

// GetLevels returns a copy of the current level distribution.
func (m *Manifest) GetLevels() [][]SSTableInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([][]SSTableInfo, len(m.levels))
	for i, level := range m.levels {
		result[i] = make([]SSTableInfo, len(level))
		copy(result[i], level)
	}
	return result
}

// NextFileNum returns the next available file number.
func (m *Manifest) NextFileNum() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nextFileNum
}

// Close releases manifest resources.
func (m *Manifest) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.file.Close()
}
