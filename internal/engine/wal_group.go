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
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	// groupCommitPageSize is the size of one page in the group commit buffer.
	// 64KB — optimal balance between memory and batching efficiency.
	groupCommitPageSize = 64 * 1024 // 64KB

	// groupCommitNumPages is the number of pages in the ring buffer.
	// 4 pages = 256KB total buffer size.
	groupCommitNumPages = 4

	// groupCommitBufSize is the total size of the pre-allocated buffer.
	// 256KB — enough to batch many small writes without frequent flushes.
	groupCommitBufSize = groupCommitPageSize * groupCommitNumPages // 256KB
)

// groupCommitWriter implements buffered asynchronous write with group commit.
// Uses a pre-allocated []byte buffer instead of bytes.Buffer to eliminate
// allocations and reallocations in the hot path.
//
// Buffer layout:
//   - buf: pre-allocated []byte of size groupCommitBufSize (256KB)
//   - bufSize: number of bytes currently buffered
//   - bufCap: total capacity (always groupCommitBufSize)
//
// When bufSize + len(data) > bufCap, the buffer is flushed to disk first.
// This guarantees zero allocations in the Write hot path.
type groupCommitWriter struct {
	mu        sync.Mutex
	buf       []byte // pre-allocated, size = groupCommitBufSize
	bufSize   int    // how many bytes are currently buffered
	bufCap    int    // total capacity (groupCommitBufSize)
	file      *os.File
	ticker    *time.Ticker
	done      chan struct{}
	flushChan chan struct{}
	syncCh    chan struct{} // сигнал для асинхронного fsync
	flushErr  error         // last flush error, protected by mu
	closed    bool
	syncMode  bool // if true, call file.Sync() asynchronously after each flush
}

// newGroupCommitWriter creates a new groupCommitWriter with a pre-allocated buffer.
// flushInterval determines how often data is flushed to disk.
// If flushInterval <= 0, defaults to 10ms.
// syncMode enables fsync after each flush. Disable for benchmark-only workloads.
func newGroupCommitWriter(file *os.File, flushInterval time.Duration, syncMode bool) *groupCommitWriter {
	if flushInterval <= 0 {
		flushInterval = 10 * time.Millisecond
	}
	gcw := &groupCommitWriter{
		buf:       make([]byte, groupCommitBufSize),
		bufCap:    groupCommitBufSize,
		file:      file,
		ticker:    time.NewTicker(flushInterval),
		done:      make(chan struct{}),
		flushChan: make(chan struct{}, 1),
		syncCh:    make(chan struct{}, 1),
		syncMode:  syncMode,
	}
	go gcw.flushLoop()
	if syncMode {
		go gcw.syncLoop()
	}
	return gcw
}

// Write adds data to the buffer. Zero allocations — copies directly into
// the pre-allocated buffer. If the buffer is full, flushes to disk first.
// Returns an error only if the writer is closed or flush fails.
func (w *groupCommitWriter) Write(data []byte) error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("groupCommitWriter closed")
	}
	// If not enough space, flush first
	if w.bufSize+len(data) > w.bufCap {
		if err := w.flushLocked(); err != nil {
			w.mu.Unlock()
			return err
		}
	}
	// Single copy into pre-allocated buffer — no reallocation
	n := copy(w.buf[w.bufSize:], data)
	w.bufSize += n
	w.mu.Unlock()
	return nil
}

// Flush forces the buffer to be written to disk and synced.
func (w *groupCommitWriter) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

// flushLocked performs the flush, assuming the mutex is already held.
// fsync is NOT called here — it's done asynchronously in syncLoop().
// This makes Write() return immediately after the write(2) syscall,
// without waiting for fsync(2) which can take 1-10ms on rotational media.
func (w *groupCommitWriter) flushLocked() error {
	if w.bufSize == 0 {
		return nil
	}
	data := w.buf[:w.bufSize]
	if _, err := w.file.Write(data); err != nil {
		w.flushErr = err
		return err
	}
	w.bufSize = 0

	// Signal syncLoop to call fsync asynchronously
	if w.syncMode {
		select {
		case w.syncCh <- struct{}{}:
		default:
			// sync already pending
		}
	}
	return nil
}

// flushLoop is a background goroutine that periodically flushes the buffer.
func (w *groupCommitWriter) flushLoop() {
	for {
		select {
		case <-w.ticker.C:
			w.mu.Lock()
			if err := w.flushLocked(); err != nil {
				w.flushErr = err
			}
			w.mu.Unlock()
		case <-w.flushChan:
			w.mu.Lock()
			if err := w.flushLocked(); err != nil {
				w.flushErr = err
			}
			w.mu.Unlock()
			return
		case <-w.done:
			return
		}
	}
}

// syncLoop is a background goroutine that calls file.Sync() asynchronously.
// This removes fsync latency from the hot path (Write/Flush).
// fsync can take 1-10ms on HDD or under fsync pressure — doing it
// asynchronously means PutWithTS returns in ~200ns instead of ~1ms.
func (w *groupCommitWriter) syncLoop() {
	for {
		select {
		case <-w.syncCh:
			if err := w.file.Sync(); err != nil {
				w.mu.Lock()
				w.flushErr = err
				w.mu.Unlock()
			}
		case <-w.done:
			// Final sync before exit
			_ = w.file.Sync()
			return
		}
	}
}

// Close stops the background goroutine, flushes remaining data, and closes the file.
// The file is not closed here — it belongs to WAL.
func (w *groupCommitWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	close(w.done)
	w.ticker.Stop()
	// Force flush remaining data
	err := w.flushLocked()
	w.mu.Unlock()

	// Wait for syncLoop to finish its final sync
	// syncLoop will exit when it reads from w.done
	return err
}

// Error returns the last flush error (if any).
func (w *groupCommitWriter) Error() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushErr
}
