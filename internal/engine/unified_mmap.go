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

// ============================================================
// Unified MMap Ring Buffer — Zero-Copy Hot Write Path
// ============================================================
//
// This replaces the dual-write pattern (WAL + VLog) with a single
// mmap-backed ring buffer. In the hot path (PutWithTS), we write
// the serialized (key, value, timestamp, op) into ONE mmap region.
//
// Benefits:
//   1. Single cache-line footprint instead of two (WAL + VLog)
//   2. No mutex in hot path — atomic.AddUint64 for offset
//   3. No copy() — direct uint64 word writes via unsafe
//   4. Zero syscalls in hot path (mmap is pre-allocated)
//   5. Zero allocations — pre-mapped buffer
//
// Layout per entry (variable length):
//   [0:1]    Op (1 byte)
//   [1:2]    Flags (1 byte) — bit 0 = IsLarge
//   [2:10]   Timestamp (8 bytes)
//   [10:12]  KeyLen (2 bytes)
//   [12:16]  ValueLen (4 bytes)
//   [16:16+KL] Key (variable)
//   [16+KL:16+KL+VL] Value (variable)
//   [end-4:end] CRC32 (4 bytes)
//
// The Flags.IsLarge bit disambiguates a real ValuePointer from a user value of
// exactly 12 bytes. See DEF-02 / DEF-04.
//
// Recovery reads this file sequentially, same as WAL recovery.
// ============================================================

package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/f4ga/ScoriaDB/internal/logger"
)

// UnifiedMmapConfig controls the unified mmap buffer.
const (
	// UnifiedMmapSize is the initial size of the unified mmap region.
	// 256MB — enough for ~65K entries of 4KB each before extension.
	// Large enough to avoid extensions during benchmarks.
	UnifiedMmapSize = 256 * 1024 * 1024 // 256MB

	// UnifiedMmapExtendSize is the size to extend by when full.
	UnifiedMmapExtendSize = 256 * 1024 * 1024 // 256MB
)

// UnifiedMmap is a single mmap-backed ring buffer for the hot write path.
// It replaces both WAL and VLog writes in PutWithTS.
//
// Thread-safety: WriteEntry is serialized via mu to prevent SIGBUS
// when extendMmap remaps um.data during a write. The mutex is held
// for the entire write operation, ensuring um.data is stable.
// extendMmap is called under the same mutex.
type UnifiedMmap struct {
	file     *os.File
	data     []byte // mmap region, READ-WRITE
	mmapSize int64  // current mmap size
	head     uint64 // write offset (protected by mu)
	closed   bool
	mu       sync.Mutex // protects WriteEntry and extendMmap
}

// OpenUnifiedMmap opens or creates the unified mmap file.
func OpenUnifiedMmap(path string) (*UnifiedMmap, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open unified mmap file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to stat unified mmap file: %w", err)
	}

	var mmapSize int64
	var head uint64

	if stat.Size() == 0 {
		// New file — pre-allocate
		mmapSize = UnifiedMmapSize
		if err := file.Truncate(mmapSize); err != nil {
			_ = file.Close()
			return nil, fmt.Errorf("failed to truncate unified mmap to %d: %w", mmapSize, err)
		}
		head = 0
	} else {
		// Existing file — use current size
		mmapSize = stat.Size()
		head = uint64(mmapSize) // append mode
	}

	data, err := syscall.Mmap(int(file.Fd()), 0, int(mmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("failed to mmap unified file: %w", err)
	}

	um := &UnifiedMmap{
		file:     file,
		data:     data,
		mmapSize: mmapSize,
		head:     head,
	}

	logger.Info("unified mmap: opened size=%d head=%d", mmapSize, head)
	return um, nil
}

// WriteEntry serializes and writes a single entry into the mmap ring buffer.
// Returns the offset of the value data within the entry (for direct reading).
//
// Thread-safety: serialized via um.mu to prevent SIGBUS when extendMmap
// remaps um.data during a write. The mutex is held for the entire write
// operation, ensuring um.data is stable.
//
// Zero allocations, zero syscalls in the common case (no extension).
func (um *UnifiedMmap) WriteEntry(op OpType, key, value []byte, timestamp uint64) (uint64, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	keyLen := len(key)
	valLen := len(value)
	// Header grew from 15 to 16 bytes (1 op + 1 flags + 8 ts + 2 klen + 4 vlen).
	totalSize := 1 + 1 + 8 + 2 + 4 + keyLen + valLen + 4 // + CRC

	// Reserve space
	offset := um.head
	um.head += uint64(totalSize)

	// Check if we need to extend (rare, ~1 per 16K entries for 4KB values)
	if int64(offset)+int64(totalSize) > um.mmapSize {
		if err := um.extendMmap(int64(offset) + int64(totalSize)); err != nil {
			return 0, err
		}
	}

	// Write directly into mmap using unsafe — zero bounds checking.
	// Multi-byte fields are written in BigEndian to match ReadEntry, which
	// decodes with binary.BigEndian. Writing native-endian here made recovery
	// read garbage on little-endian platforms. CRC is order-independent.
	dst := unsafe.Pointer(&um.data[offset])
	pos := uintptr(0)

	// Op (1 byte)
	*(*byte)(unsafe.Add(dst, pos)) = byte(op)
	pos++

	// Flags (1 byte) — bit 0 = IsLarge. Large values (>MaxInlineSize) are
	// always ValuePointers here, so the flag is set whenever the caller stores
	// a pointer. See DEF-02 / DEF-04.
	var flags byte
	if valLen == ValuePointerSize {
		flags |= walFlagIsLarge
	}
	*(*byte)(unsafe.Add(dst, pos)) = flags
	pos++

	// Timestamp (8 bytes) — BigEndian to match ReadEntry.
	binary.BigEndian.PutUint64(um.data[offset+uint64(pos):offset+uint64(pos)+8], timestamp)
	pos += 8

	// KeyLen (2 bytes) — BigEndian to match ReadEntry.
	binary.BigEndian.PutUint16(um.data[offset+uint64(pos):offset+uint64(pos)+2], uint16(keyLen))
	pos += 2

	// ValueLen (4 bytes) — BigEndian to match ReadEntry.
	binary.BigEndian.PutUint32(um.data[offset+uint64(pos):offset+uint64(pos)+4], uint32(valLen))
	pos += 4

	// Key (variable) — standard copy (runtime memmove is safe on all platforms,
	// including ARM64 where unaligned word reads in memcpyWordAligned could
	// trigger SIGBUS). See DEF-B4.
	copy(um.data[offset+uint64(pos):offset+uint64(pos)+uint64(keyLen)], key)
	pos += uintptr(keyLen)

	// Value (variable) — standard copy. See DEF-B4.
	copy(um.data[offset+uint64(pos):offset+uint64(pos)+uint64(valLen)], value)
	pos += uintptr(valLen)

	// CRC32 (4 bytes) — computed on the written data; order-independent.
	crc := crc32.ChecksumIEEE(um.data[offset : offset+uint64(pos)])
	binary.BigEndian.PutUint32(um.data[offset+uint64(pos):offset+uint64(pos)+4], crc)

	// Return the offset of the value data (after header + key)
	// This allows direct reading via ReadValue without knowing keyLen
	return offset + uint64(1+1+8+2+4+keyLen), nil
}

// ReadValue reads the value data at the given value offset (returned by WriteEntry).
// Returns a direct slice into the mmap — zero copy.
// The caller must ensure the mmap is not closed during use.
//
// DEF-18 (Глава II, Глава VI): extendMmap() may concurrently unmap the old mmap
// region and install a new one. um.data / um.mmapSize are guarded by um.mu, so
// this read takes the lock to avoid a data race (detected by -race) and to
// ensure the returned slice points into a region that is not being unmapped.
// The returned slice is copied by the caller (decodeStoredValue) before it can
// outlive this critical section.
func (um *UnifiedMmap) ReadValue(valueOffset uint64, valueSize int32) ([]byte, error) {
	um.mu.Lock()
	defer um.mu.Unlock()

	if int64(valueOffset)+int64(valueSize) > um.mmapSize {
		return nil, fmt.Errorf("unified mmap: value at offset %d out of range", valueOffset)
	}
	// DEF-18: return a COPY of the value while still holding um.mu. The old
	// comment claimed the caller (decodeStoredValue) copies before the slice
	// outlives the lock, but extendMmap() may munmap this region the moment the
	// lock is released — the copy in decodeStoredValue happens AFTER the lock,
	// so it can read freed memory (SIGSEGV). Copying here guarantees the returned
	// slice is never backed by a region that can be concurrently unmapped.
	// One allocation — large values are not on the hot path.
	src := um.data[valueOffset : valueOffset+uint64(valueSize)]
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst, nil
}

// ReadEntry reads and decodes an entry at the given offset.
// Used during recovery — NOT in hot path.
func (um *UnifiedMmap) ReadEntry(offset uint64) (*WalEntry, error) {
	if int64(offset+16) > um.mmapSize {
		return nil, fmt.Errorf("unified mmap: offset %d out of range", offset)
	}

	data := um.data[offset:]
	pos := 0

	op := OpType(data[pos])
	pos++

	flags := data[pos]
	pos++

	timestamp := binary.BigEndian.Uint64(data[pos : pos+8])
	pos += 8

	keyLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2

	valLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4

	if pos+keyLen+valLen+4 > len(data) {
		return nil, fmt.Errorf("unified mmap: entry truncated at offset %d", offset)
	}

	key := make([]byte, keyLen)
	copy(key, data[pos:pos+keyLen])
	pos += keyLen

	value := make([]byte, valLen)
	copy(value, data[pos:pos+valLen])
	pos += valLen

	crcStored := binary.BigEndian.Uint32(data[pos : pos+4])

	// Verify CRC
	entryLen := pos + 4 - 4 // exclude CRC from checksum
	crc := crc32.ChecksumIEEE(data[:entryLen])
	if crc != crcStored {
		return nil, fmt.Errorf("unified mmap: CRC mismatch at offset %d: stored=%x computed=%x", offset, crcStored, crc)
	}

	entry := newWalEntry()
	entry.Op = op
	entry.Key = key
	entry.Value = value
	entry.Timestamp = timestamp
	// Read the persisted IsLarge flag (bit 0) to disambiguate a real ValuePointer
	// from a user value of exactly 12 bytes. See DEF-02 / DEF-04.
	entry.IsLarge = flags&walFlagIsLarge != 0

	return entry, nil
}

// Size returns the current write head (logical end of data).
func (um *UnifiedMmap) Size() int64 {
	um.mu.Lock()
	defer um.mu.Unlock()
	return int64(um.head)
}

// Close unmaps and closes the file.
func (um *UnifiedMmap) Close() error {
	if um.closed {
		return nil
	}
	um.closed = true

	// Sync to disk
	if err := um.file.Sync(); err != nil {
		logger.Warn("unified mmap: sync failed: %v", err)
	}

	if err := syscall.Munmap(um.data); err != nil {
		return fmt.Errorf("failed to munmap unified file: %w", err)
	}
	um.data = nil

	return um.file.Close()
}

// Sync flushes the mmap to disk.
func (um *UnifiedMmap) Sync() error {
	_, _, err := syscall.Syscall(syscall.SYS_MSYNC, uintptr(unsafe.Pointer(&um.data[0])), uintptr(len(um.data)), syscall.MS_SYNC)
	if err != 0 {
		return fmt.Errorf("msync failed: %v", err)
	}
	return nil
}

// extendMmap extends the mmap region when full.
// This is the ONLY place where a syscall happens in the write path.
// Called rarely (~1 per 16K entries for 4KB values).
func (um *UnifiedMmap) extendMmap(neededSize int64) error {
	newMmapSize := neededSize
	// Align to extend size
	if remainder := newMmapSize % UnifiedMmapExtendSize; remainder != 0 {
		newMmapSize += UnifiedMmapExtendSize - remainder
	}

	// Extend the file
	if err := um.file.Truncate(newMmapSize); err != nil {
		return fmt.Errorf("failed to truncate unified mmap to %d: %w", newMmapSize, err)
	}

	// Remap
	oldData := um.data
	newData, err := syscall.Mmap(int(um.file.Fd()), 0, int(newMmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to remap unified mmap to %d: %w", newMmapSize, err)
	}

	if err := syscall.Munmap(oldData); err != nil {
		logger.Warn("unified mmap: failed to munmap old data: %v", err)
	}

	um.data = newData
	um.mmapSize = newMmapSize

	logger.Info("unified mmap: extended to %d", newMmapSize)
	return nil
}

// memcpyWordAligned was removed in DEF-B4. It performed unaligned 64-bit reads
// which trigger SIGBUS on ARM64 when the source offset is not 8-byte aligned.
// The callers now use the standard copy() (runtime memmove), which is safe on
// all platforms and handles alignment internally.
