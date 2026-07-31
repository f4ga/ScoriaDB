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

//go:build !linux && !darwin && !windows

package sstable

import (
	"fmt"
	"os"
)

// mmapFile is the fallback implementation of MmapFile for platforms
// that do not support mmap (e.g., FreeBSD, NetBSD, Solaris, etc.).
//
// This implementation uses traditional file I/O (os.File) as a fallback.
// Data() returns nil, and all reads go through ReadAt().
//
// Performance on these platforms will be lower due to syscall overhead
// and memory allocations, but correctness is guaranteed.
//
// See: ARCH-MMAP-05
type mmapFile struct {
	file *os.File
	size int64
}

// openMmapFile opens a file for reading on platforms without mmap.
// Uses os.Open and os.File.ReadAt for all I/O.
//
// See: ARCH-MMAP-05
func openMmapFile(path string) (*mmapFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file (fallback): %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return &mmapFile{
		file: file,
		size: stat.Size(),
	}, nil
}

// Data returns nil on fallback platforms — mmap is not available.
// Callers must use ReadAt() instead.
func (m *mmapFile) Data() []byte {
	return nil
}

// Size returns the file size.
func (m *mmapFile) Size() int64 {
	return m.size
}

// ReadAt reads len(p) bytes from the file starting at offset off.
// Uses os.File.ReadAt which performs a syscall per call.
//
// See: ARCH-MMAP-03
func (m *mmapFile) ReadAt(p []byte, off int64) (int, error) {
	return m.file.ReadAt(p, off)
}

// Close closes the underlying file.
//
// Safe to call multiple times (idempotent after first call).
//
// See: ARCH-MMAP-04
func (m *mmapFile) Close() error {
	if m.file == nil {
		return nil
	}
	err := m.file.Close()
	m.file = nil
	return err
}
