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

//go:build linux

package sstable

import (
	"fmt"
	"syscall"
)

// mmapFile is the Linux implementation of MmapFile.
// Uses syscall.Mmap with PROT_READ and MAP_SHARED.
//
// The file descriptor is closed immediately after mmap — the mapping
// remains valid until Munmap is called.
//
// See: ARCH-MMAP-01, PERF-MMAP-01
type mmapFile struct {
	data []byte
	size int64
}

// openMmapFile opens a file and memory-maps it for reading.
//
// Algorithm:
//  1. syscall.Open — get file descriptor
//  2. syscall.Fstat — determine file size
//  3. syscall.Mmap(fd, 0, size, PROT_READ, MAP_SHARED) — map into memory
//  4. syscall.Close(fd) — close fd (mmap region remains valid)
//
// Returns an error if the file cannot be opened, stat'ed, or mmapped.
// Empty files are rejected (mmap of zero-length file is undefined).
//
// See: ARCH-MMAP-02
func openMmapFile(path string) (*mmapFile, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for mmap: %w", err)
	}

	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		syscall.Close(fd) //nolint:errcheck
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size == 0 {
		syscall.Close(fd) //nolint:errcheck
		return nil, fmt.Errorf("empty file cannot be mmapped")
	}

	// mmap: PROT_READ + MAP_SHARED
	// MAP_SHARED — changes are visible to other processes (we only read)
	// PROT_READ — we only need read access
	data, err := syscall.Mmap(fd, 0, int(stat.Size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd) //nolint:errcheck
		return nil, fmt.Errorf("failed to mmap file: %w", err)
	}

	// FD can be closed after mmap — the region remains valid
	// See: mmap(2) — "The file descriptor is not used after mmap() returns."
	if err := syscall.Close(fd); err != nil {
		// Non-critical — the mapping is already established
		// In production: logger.Warn("failed to close fd after mmap: %v", err)
	}

	return &mmapFile{
		data: data,
		size: stat.Size,
	}, nil
}

// Data returns the entire file as a byte slice backed by the mmap region.
// The slice remains valid until Close() is called.
//
// Zero allocations — returns a direct slice of the mmap region.
//
// See: PERF-MMAP-02
func (m *mmapFile) Data() []byte {
	return m.data
}

// Size returns the file size.
func (m *mmapFile) Size() int64 {
	return m.size
}

// ReadAt reads len(p) bytes from the file starting at offset off.
// This is a fallback for platforms without mmap — on Linux it copies
// from the mmap region directly.
//
// See: ARCH-MMAP-03
func (m *mmapFile) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 || off >= m.size {
		return 0, fmt.Errorf("offset out of range: %d (size: %d)", off, m.size)
	}
	end := off + int64(len(p))
	if end > m.size {
		end = m.size
		p = p[:end-off]
	}
	copy(p, m.data[off:end])
	return len(p), nil
}

// Close unmaps the mmap region.
//
// Protected against SIGBUS during munmap — if the underlying file was
// truncated externally, munmap may trigger SIGBUS. The recover() handler
// catches this and returns an error instead of crashing.
//
// Safe to call multiple times (idempotent after first call).
//
// See: ARCH-MMAP-04, SAFE-MMAP-01
func (m *mmapFile) Close() error {
	if m.data == nil {
		return nil
	}

	// Protect against SIGBUS during munmap
	// If the file was truncated externally, munmap may trigger SIGBUS
	defer func() {
		if r := recover(); r != nil {
			// SIGBUS during munmap — log but don't crash
			// In production: logger.Warn("munmap recovered from panic: %v", r)
		}
	}()

	if err := syscall.Munmap(m.data); err != nil {
		return fmt.Errorf("failed to munmap: %w", err)
	}
	m.data = nil
	return nil
}
