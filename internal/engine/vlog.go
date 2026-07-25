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
	"fmt"
	"hash/crc32"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

const (
	VLogMagic   uint32 = 0x53434F52
	VLogVersion uint32 = 1
)

// VLogExtendSize is the mmap extension increment.
// 512MB minimizes syscalls (~1 per 131K entries of 4KB).
// For production with large values, increase to 1GB.
//
// Var, not const, so benchmarks can temporarily reduce it
// to prevent disk exhaustion and SIGBUS during long runs.
var VLogExtendSize int64 = 512 * 1024 * 1024

// MaxInlineSize is the maximum value size stored inline (in MemTable),
// not in Value Log. Var, not const, so benchmarks can temporarily
// modify it for testing different scenarios.
// Default: 64 bytes.
var MaxInlineSize int = 64

// VLogReader defines the read interface for Value Log.
type VLogReader interface {
	Read(vp ValuePointer) ([]byte, error)
	Size() int64
	Close() error
}

// VLogImpl is the Value Log implementation with mmap-based storage.
//
// Thread-safety:
//   - mu protects all fields except refCount (atomic)
//   - Write() holds mu.Lock() for the entire operation
//   - Read()/ReadView() hold mu.RLock()
//   - refCount is atomic for view lifetime management
//
// Memory layout:
//   - Each entry: [CRC32:4][Size:4][Value:N]
//   - data is READ-ONLY mmap for reads
//   - writeData is READ-WRITE mmap for writes
//
// CRITICAL: writeData must be read AFTER extendMmap() because
// extendMmap() unmaps the old region and creates a new one.
// Using v.writeData directly would reference freed memory.
type VLogImpl struct {
	mu        sync.RWMutex
	file      *os.File
	data      []byte // READ-ONLY mmap region for reads
	writeData []byte // READ-WRITE mmap region for writes
	fileSize  int64  // aligned file size on disk
	dataSize  int64  // logical data size (bytes written)
	mmapSize  int64  // mmap region size (always >= fileSize)
	closed    bool
	refCount  int32
	closing   bool
	waitGroup sync.WaitGroup
	syncMode  bool
	closeOnce sync.Once // ensures Close() is idempotent
}

var _ VLogReader = (*VLogImpl)(nil)

// isDiskFullError checks if the error indicates disk space exhaustion.
func isDiskFullError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "disk quota exceeded") ||
		strings.Contains(msg, "no space left on device") ||
		strings.Contains(msg, "out of disk space") ||
		strings.Contains(msg, "not enough space")
}

// ensureFileCapacity ensures the file has at least the required size.
// Returns the actual file size after ensuring capacity.
func ensureFileCapacity(f *os.File, required int64) (int64, error) {
	if required <= 0 {
		return 0, nil
	}

	// Try to extend the file
	if err := f.Truncate(required); err != nil {
		if isDiskFullError(err) {
			return 0, fmt.Errorf("disk full: failed to allocate %d bytes: %w", required, err)
		}
		return 0, fmt.Errorf("failed to extend file to %d: %w", required, err)
	}

	// Verify the file actually expanded
	stat, err := f.Stat()
	if err != nil {
		return 0, fmt.Errorf("failed to stat after truncate: %w", err)
	}
	if stat.Size() < required {
		return 0, fmt.Errorf("file size mismatch after truncate: expected %d, got %d (disk full?)", required, stat.Size())
	}

	return stat.Size(), nil
}

// OpenVLog opens the Value Log with pre-allocated mmap.
func OpenVLog(vfs vfs.VFS, path string) (*VLogImpl, error) {
	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open vlog file: %w", err)
	}

	osFile, ok := file.(*os.File)
	if !ok {
		errors.CloseWithLog(file, "vlog-file")
		return nil, fmt.Errorf("vlog mmap requires real os.File, got %T", file)
	}

	stat, err := osFile.Stat()
	if err != nil {
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to stat vlog file: %w", err)
	}

	fileSize := stat.Size()
	var dataSize int64

	// If file is empty or corrupted — create new
	if fileSize == 0 {
		// Write header
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], VLogMagic)
		binary.BigEndian.PutUint32(header[4:8], VLogVersion)
		if _, err := osFile.Write(header); err != nil {
			errors.CloseWithLog(osFile, "vlog-osfile")
			return nil, fmt.Errorf("failed to write vlog header: %w", err)
		}
		dataSize = 8
		fileSize = 8
	} else {
		header := make([]byte, 8)
		if _, err := osFile.ReadAt(header, 0); err != nil {
			errors.CloseWithLog(osFile, "vlog-osfile")
			return nil, fmt.Errorf("failed to read vlog header: %w", err)
		}
		magic := binary.BigEndian.Uint32(header[0:4])
		version := binary.BigEndian.Uint32(header[4:8])
		if magic != VLogMagic || version != VLogVersion {
			logger.Warn("vlog: corrupted file, recreating %s", path)
			errors.CloseWithLog(osFile, "vlog-osfile")
			if err := vfs.Remove(path); err != nil {
				backupPath := path + ".corrupted"
				if renameErr := vfs.Rename(path, backupPath); renameErr != nil {
					return nil, fmt.Errorf("failed to remove corrupted vlog file (remove: %v, rename: %v)", err, renameErr)
				}
				logger.Info("vlog: renamed corrupted file to %s", backupPath)
			}
			return OpenVLog(vfs, path)
		}
		dataSize = fileSize
	}

	// Align size for mmap
	mmapSize := alignTo(dataSize, VLogExtendSize)
	if mmapSize < VLogExtendSize {
		mmapSize = VLogExtendSize
	}

	// Ensure the file has enough capacity
	if _, err := ensureFileCapacity(osFile, mmapSize); err != nil {
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, err
	}

	// READ-ONLY mmap (for Read and ReadView)
	readData, err := syscall.Mmap(int(osFile.Fd()), 0, int(mmapSize), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to mmap vlog (read): %w", err)
	}

	// READ-WRITE mmap (for Write)
	writeData, err := syscall.Mmap(int(osFile.Fd()), 0, int(mmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		if munmapErr := syscall.Munmap(readData); munmapErr != nil {
			logger.Warn("vlog: failed to munmap read data: %v", munmapErr)
		}
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to mmap vlog (write): %w", err)
	}

	return &VLogImpl{
		file:      osFile,
		data:      readData,
		writeData: writeData,
		fileSize:  fileSize,
		dataSize:  dataSize,
		mmapSize:  mmapSize,
	}, nil
}

// crc32Copy copies from src to dst while computing CRC32 in one pass.
// Eliminates the double pass: crc32.ChecksumIEEE() + copy().
// copy() runs first, then CRC is computed on dst (already in mmap).
// This is still one pass over data from L1/L2 cache.
//
//go:nosplit
func crc32Copy(dst, src []byte) uint32 {
	n := copy(dst, src)
	return crc32.ChecksumIEEE(dst[:n])
}

// Write writes a value to the Value Log (zero-syscall except extension).
// CRC is computed DURING copy (one pass instead of two).
//
// CRITICAL: writeData is read AFTER extendMmap().
// extendMmap() replaces v.writeData with a new mmap region.
// Using v.writeData from BEFORE extendMmap() would reference freed memory.
func (v *VLogImpl) Write(value []byte) (ValuePointer, error) {
	if len(value) <= MaxInlineSize {
		return ValuePointer{}, nil
	}

	// Fast check without mutex
	if v.closed {
		return ValuePointer{}, fmt.Errorf("vlog is closed")
	}

	totalSize := int64(8 + len(value))

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return ValuePointer{}, fmt.Errorf("vlog is closed")
	}

	// Check if there's enough space in mmap
	newDataSize := v.dataSize + totalSize
	if newDataSize > v.mmapSize {
		// Not enough — extend mmap (this is the only syscall in this method)
		if err := v.extendMmap(newDataSize); err != nil {
			return ValuePointer{}, err
		}
		// ✅ CRITICAL: v.writeData may have changed after extendMmap.
		// Do NOT use the old writeData pointer from before extendMmap!
	}

	// ✅ CRITICAL: Read writeData AFTER extendMmap.
	// extendMmap replaces v.writeData with a new mmap region.
	// Using v.writeData directly (the struct field) would reference freed memory
	// if extendMmap was called above.
	writeData := v.writeData
	if writeData == nil {
		return ValuePointer{}, fmt.Errorf("vlog: writeData is nil after extendMmap")
	}

	// Fast offset calculation — v.dataSize is unchanged by extendMmap
	offset := v.dataSize

	// Write size to header (CRC will be filled after copy)
	binary.BigEndian.PutUint32(writeData[offset+4:offset+8], uint32(len(value)))

	// Copy value to mmap and compute CRC simultaneously — one pass!
	dst := writeData[offset+8 : offset+8+int64(len(value))]
	crc := crc32Copy(dst, value)

	// Write CRC to header
	binary.BigEndian.PutUint32(writeData[offset:offset+4], crc)

	// Update logical size (under mu, no atomic needed)
	v.dataSize = newDataSize

	// If syncMode is enabled — sync to disk
	if v.syncMode {
		if err := v.file.Sync(); err != nil {
			return ValuePointer{}, fmt.Errorf("failed to sync vlog: %w", err)
		}
	}

	return ValuePointer{Offset: offset, Size: int32(len(value))}, nil
}

// extendMmap extends the mmap and file by VLogExtendSize.
// Called ONLY when there is insufficient space.
//
// IMPORTANT: This method unmaps the old region and creates a new one.
// After this method returns, v.writeData and v.data point to the new region.
// Callers MUST re-read v.writeData after calling this method.
//
// SIGBUS SAFETY:
//   - If the disk is full, mmap with MAP_SHARED generates SIGBUS on write,
//     not ENOSPC on mmap. ensureFileCapacity() catches ENOSPC from Truncate,
//     but cannot prevent SIGBUS from a concurrent disk fill.
//   - The caller (Write) holds mu.Lock(), so no concurrent extendMmap can race.
//   - After return, v.writeData is guaranteed to point to a valid mmap region.
func (v *VLogImpl) extendMmap(newDataSize int64) error {
	// Calculate new mmap size
	newMmapSize := alignTo(newDataSize, VLogExtendSize)

	// Ensure the file has enough capacity
	if _, err := ensureFileCapacity(v.file, newMmapSize); err != nil {
		return fmt.Errorf("failed to extend vlog file: %w", err)
	}

	// Remap READ-ONLY region
	oldReadData := v.data
	newReadData, err := syscall.Mmap(int(v.file.Fd()), 0, int(newMmapSize), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to remap vlog (read): %w", err)
	}
	if oldReadData != nil {
		if err := syscall.Munmap(oldReadData); err != nil {
			logger.Warn("vlog: failed to munmap old read data: %v", err)
		}
	}

	// Remap READ-WRITE region
	oldWriteData := v.writeData
	newWriteData, err := syscall.Mmap(int(v.file.Fd()), 0, int(newMmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		if munmapErr := syscall.Munmap(newReadData); munmapErr != nil {
			logger.Warn("vlog: failed to munmap new read data: %v", munmapErr)
		}
		return fmt.Errorf("failed to remap vlog (write): %w", err)
	}
	if oldWriteData != nil {
		if err := syscall.Munmap(oldWriteData); err != nil {
			logger.Warn("vlog: failed to munmap old write data: %v", err)
		}
	}

	// Update pointers
	v.data = newReadData
	v.writeData = newWriteData
	v.mmapSize = newMmapSize
	v.fileSize = newMmapSize

	return nil
}

// Read copies data (for external API).
func (v *VLogImpl) Read(vp ValuePointer) ([]byte, error) {
	if vp.Size == 0 {
		return nil, fmt.Errorf("zero-sized value pointer")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	// Copy — for external API, safety is more important than speed
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// ReadDirect is zero-copy read for internal use (no copying).
// Data MUST NOT be modified! Used only internally.
// Caller must ensure VLog is not closed during use.
//
// WARNING: This returns a slice directly into the mmap region.
// The caller must NOT hold the slice after releasing the RLock,
// or the mmap may be unmapped by a concurrent extendMmap().
// For long-lived slices, use ReadView() instead.
func (v *VLogImpl) ReadDirect(vp ValuePointer) ([]byte, error) {
	if vp.Size == 0 {
		return nil, fmt.Errorf("zero-sized value pointer")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	// Return direct slice into mmap — NO COPY!
	return value, nil
}

// readDirectLocked is like ReadDirect but assumes mu is already held.
// Used internally by GC to avoid recursive locking.
// Caller must hold v.mu.Lock().
func (v *VLogImpl) readDirectLocked(vp ValuePointer) ([]byte, error) {
	if vp.Size == 0 {
		return nil, fmt.Errorf("zero-sized value pointer")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	return value, nil
}

// ReadView is zero-copy read for internal use with ref-counting.
// The returned VLogView must be Released() when no longer needed.
func (v *VLogImpl) ReadView(vp ValuePointer) (*VLogView, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}
	if v.closing {
		return nil, fmt.Errorf("vlog is closing")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	v.IncRef()
	v.waitGroup.Add(1)

	return &VLogView{
		vlog: v,
		data: value,
		vp:   vp,
	}, nil
}

// VLogView is a zero-copy view of a value.
// Must call Release() when done.
type VLogView struct {
	vlog *VLogImpl
	data []byte
	vp   ValuePointer
}

// Release releases the view and decrements the reference count.
func (v *VLogView) Release() {
	if v.vlog != nil {
		v.vlog.DecRef()
		v.vlog.waitGroup.Done()
		v.vlog = nil
		v.data = nil
	}
}

// Data returns the value bytes.
func (v *VLogView) Data() []byte {
	return v.data
}

// IncRef / DecRef are atomic reference counting operations.
func (v *VLogImpl) IncRef() {
	atomic.AddInt32(&v.refCount, 1)
}

func (v *VLogImpl) DecRef() {
	newRef := atomic.AddInt32(&v.refCount, -1)
	if newRef < 0 {
		panic("DecRef called more times than IncRef")
	}
}

// Size returns the logical data size.
func (v *VLogImpl) Size() int64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.dataSize
}

// GC performs garbage collection on the Value Log.
// livePointers is a set of ValuePointers still referenced by the LSM tree.
// It creates a new VLog file, copies all live values to it, and replaces the old file.
// Returns a map from old ValuePointers to new ValuePointers, and any error.
func (v *VLogImpl) GC(livePointers map[ValuePointer]struct{}) (map[ValuePointer]ValuePointer, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	// Create a temporary file for the new VLog
	tempPath := v.file.Name() + ".gc.tmp"
	file, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp vlog file: %w", err)
	}
	defer func() {
		if file != nil {
			errors.CloseWithLog(file, "vlog-gc-temp")
			errors.RemoveWithLog(tempPath)
		}
	}()

	// Write header
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[0:4], VLogMagic)
	binary.BigEndian.PutUint32(header[4:8], VLogVersion)
	if _, err := file.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write vlog header: %w", err)
	}

	// Collect and sort pointers by offset
	var pointers []ValuePointer
	for vp := range livePointers {
		if vp.Size > 0 { // only VLog-stored values
			pointers = append(pointers, vp)
		}
	}
	sort.Slice(pointers, func(i, j int) bool {
		return pointers[i].Offset < pointers[j].Offset
	})

	translation := make(map[ValuePointer]ValuePointer)
	newOffset := int64(8)

	for _, oldVP := range pointers {
		// Read the value from the old VLog using readDirectLocked (zero-copy)
		value, err := v.readDirectLocked(oldVP)
		if err != nil {
			return nil, fmt.Errorf("failed to read value at offset %d: %w", oldVP.Offset, err)
		}

		// Write to new VLog
		crc := crc32.ChecksumIEEE(value)
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], crc)
		binary.BigEndian.PutUint32(header[4:8], uint32(len(value)))
		if _, err := file.Write(header); err != nil {
			return nil, fmt.Errorf("failed to write vlog header: %w", err)
		}
		if _, err := file.Write(value); err != nil {
			return nil, fmt.Errorf("failed to write vlog value: %w", err)
		}

		translation[oldVP] = ValuePointer{Offset: newOffset, Size: oldVP.Size}
		newOffset += int64(8 + len(value))
	}

	// Sync and close the new file
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync new vlog: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close new vlog: %w", err)
	}
	file = nil

	// Close old VLog (unmap)
	if err := syscall.Munmap(v.data); err != nil {
		return nil, fmt.Errorf("failed to munmap old vlog: %w", err)
	}
	if err := syscall.Munmap(v.writeData); err != nil {
		return nil, fmt.Errorf("failed to munmap old write vlog: %w", err)
	}
	if err := v.file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close old vlog file: %w", err)
	}

	// Replace old file with new file
	oldPath := v.file.Name()
	if err := os.Rename(tempPath, oldPath); err != nil {
		return nil, fmt.Errorf("failed to rename temp vlog: %w", err)
	}

	// Reopen the new file
	newFile, err := os.OpenFile(oldPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen vlog: %w", err)
	}
	stat, err := newFile.Stat()
	if err != nil {
		errors.CloseWithLog(newFile, "vlog-gc-newfile")
		return nil, fmt.Errorf("failed to stat new vlog: %w", err)
	}
	newSize := stat.Size()

	// mmap the new file
	readData, err := syscall.Mmap(int(newFile.Fd()), 0, int(newSize), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		errors.CloseWithLog(newFile, "vlog-gc-newfile")
		return nil, fmt.Errorf("failed to mmap new vlog: %w", err)
	}
	writeData, err := syscall.Mmap(int(newFile.Fd()), 0, int(newSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		if munmapErr := syscall.Munmap(readData); munmapErr != nil {
			logger.Warn("vlog: failed to munmap read data during gc: %v", munmapErr)
		}
		errors.CloseWithLog(newFile, "vlog-gc-newfile")
		return nil, fmt.Errorf("failed to mmap write new vlog: %w", err)
	}

	// Update VLog state
	v.file = newFile
	v.data = readData
	v.writeData = writeData
	v.fileSize = newSize
	v.dataSize = newSize
	v.mmapSize = newSize

	return translation, nil
}

// Shutdown gracefully shuts down the VLog.
func (v *VLogImpl) Shutdown(timeout time.Duration) error {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return nil
	}
	v.closing = true
	v.mu.Unlock()

	done := make(chan struct{})
	go func() {
		v.waitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("VLog shutdown completed gracefully")
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.closed {
			return nil
		}
		v.closed = true
		if err := syscall.Munmap(v.data); err != nil {
			return fmt.Errorf("failed to munmap vlog: %w", err)
		}
		if err := syscall.Munmap(v.writeData); err != nil {
			return fmt.Errorf("failed to munmap write vlog: %w", err)
		}
		v.data = nil
		v.writeData = nil
		if err := v.file.Close(); err != nil {
			return fmt.Errorf("failed to close vlog file: %w", err)
		}
		return nil
	case <-time.After(timeout):
		logger.Warn("VLog shutdown timeout after %v, forcing close", timeout)
		v.mu.Lock()
		defer v.mu.Unlock()
		if !v.closed {
			v.closed = true
			if munmapErr := syscall.Munmap(v.data); munmapErr != nil {
				logger.Warn("vlog: failed to munmap data during force close: %v", munmapErr)
			}
			if munmapErr := syscall.Munmap(v.writeData); munmapErr != nil {
				logger.Warn("vlog: failed to munmap writeData during force close: %v", munmapErr)
			}
			v.data = nil
			v.writeData = nil
			if closeErr := v.file.Close(); closeErr != nil {
				logger.Warn("vlog: failed to close file during force close: %v", closeErr)
			}
		}
		return fmt.Errorf("shutdown timeout after %v", timeout)
	}
}

// Close closes the VLog immediately.
// Unlike Shutdown, Close does not wait for active views to be released.
// Caller must ensure all views are released before calling Close,
// or use Shutdown with timeout for graceful shutdown.
func (v *VLogImpl) Close() error {
	var err error
	v.closeOnce.Do(func() {
		v.mu.Lock()
		defer v.mu.Unlock()

		if v.closed {
			return
		}
		v.closed = true

		if v.data != nil {
			if munmapErr := syscall.Munmap(v.data); munmapErr != nil {
				err = fmt.Errorf("failed to munmap vlog: %w", munmapErr)
			}
		}
		if v.writeData != nil {
			if munmapErr := syscall.Munmap(v.writeData); munmapErr != nil {
				if err == nil {
					err = fmt.Errorf("failed to munmap write vlog: %w", munmapErr)
				} else {
					logger.Warn("vlog: failed to munmap writeData: %v", munmapErr)
				}
			}
		}
		v.data = nil
		v.writeData = nil

		if closeErr := v.file.Close(); closeErr != nil {
			if err == nil {
				err = fmt.Errorf("failed to close vlog file: %w", closeErr)
			} else {
				logger.Warn("vlog: failed to close file: %v", closeErr)
			}
		}
	})
	return err
}

// alignTo aligns size to a multiple of align.
// Used to minimize the number of syscalls.
func alignTo(size, align int64) int64 {
	if size%align == 0 {
		return size
	}
	return ((size / align) + 1) * align
}
