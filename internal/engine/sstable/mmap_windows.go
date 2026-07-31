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

//go:build windows

package sstable

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// mmapFile is the Windows implementation of MmapFile.
// Uses Windows API: CreateFileMapping + MapViewOfFile.
//
// Windows does not have a direct mmap syscall. Instead, we use:
//  1. CreateFile — open the file
//  2. CreateFileMapping — create a file mapping object
//  3. MapViewOfFile — map the file into memory
//  4. CloseHandle — close the file handle (mapping remains valid)
//
// See: ARCH-MMAP-01, PERF-MMAP-01
type mmapFile struct {
	data   []byte
	size   int64
	handle syscall.Handle // file mapping handle (for UnmapViewOfFile)
	file   *os.File       // file handle (closed after mapping)
}

// openMmapFile opens a file and memory-maps it for reading on Windows.
//
// Algorithm:
//  1. os.Open — open the file
//  2. file.Stat — determine file size
//  3. CreateFileMapping — create a read-only file mapping
//  4. MapViewOfFile — map the file into memory
//  5. CloseHandle(file handle) — close the file handle
//
// Returns an error if any step fails.
// Empty files are rejected.
//
// See: ARCH-MMAP-02
func openMmapFile(path string) (*mmapFile, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file for mmap: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	if stat.Size() == 0 {
		file.Close()
		return nil, fmt.Errorf("empty file cannot be mmapped")
	}

	size := stat.Size()

	// Get the underlying file handle
	// On Windows, we need the HANDLE for CreateFileMapping
	// We use syscall.Handle(file.Fd()) but must prevent the file from
	// being closed while we use the handle
	//nolint:govet
	fd := file.Fd()
	handle, err := syscall.CreateFileMapping(
		syscall.Handle(fd),
		nil,                   // security attributes
		syscall.PAGE_READONLY, // protection
		uint32(size>>32),      // high 32 bits of size
		uint32(size),          // low 32 bits of size
		nil,                   // name (unnamed mapping)
	)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to create file mapping: %w", err)
	}

	// Map the file into memory
	addr, err := syscall.MapViewOfFile(
		handle,
		syscall.FILE_MAP_READ, // read-only access
		0,                     // high 32 bits of offset
		0,                     // low 32 bits of offset
		uintptr(size),         // number of bytes to map
	)
	if err != nil {
		syscall.CloseHandle(handle)
		file.Close()
		return nil, fmt.Errorf("failed to map view of file: %w", err)
	}

	// Create a byte slice backed by the mapped memory
	// The slice header points directly to the mapped region
	var data []byte
	sliceHeader := (*struct {
		addr uintptr
		len  int
		cap  int
	})(unsafe.Pointer(&data))
	sliceHeader.addr = addr
	sliceHeader.len = int(size)
	sliceHeader.cap = int(size)

	// Close the file handle — the mapping remains valid
	file.Close()

	return &mmapFile{
		data:   data,
		size:   size,
		handle: handle,
	}, nil
}

// Data returns the entire file as a byte slice backed by the mmap region.
// The slice remains valid until Close() is called.
//
// Zero allocations — returns a direct slice of the mapped region.
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
// This is a fallback — on Windows it copies from the mapped region directly.
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

// Close unmaps the file mapping and closes the mapping handle.
//
// Safe to call multiple times (idempotent after first call).
//
// See: ARCH-MMAP-04
func (m *mmapFile) Close() error {
	if m.data == nil {
		return nil
	}

	// Unmap the view
	addr := uintptr(unsafe.Pointer(&m.data[0]))
	if err := syscall.UnmapViewOfFile(addr); err != nil {
		return fmt.Errorf("failed to unmap view of file: %w", err)
	}

	// Close the file mapping handle
	if err := syscall.CloseHandle(m.handle); err != nil {
		return fmt.Errorf("failed to close file mapping handle: %w", err)
	}

	m.data = nil
	m.handle = 0
	return nil
}
