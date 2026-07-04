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
// Linear Arena Allocator — Zero-Allocation Hot Path
// ============================================================
//
// This arena provides lock-free allocations for the skip list hot path.
// Memory is managed as a grow-only linear allocator:
//   - Alloc() uses CAS to reserve space — no mutex in hot path
//   - grow() uses mutex only when expanding to a new block
//   - No free() until Reset() — memory is never recycled mid-session
//   - Zero GC pressure: all memory is off-heap from Go's perspective
// ============================================================

// Package memtable provides an in-memory table implementation using a lock-free
// skip list with a linear arena allocator. It supports concurrent reads and writes
// with zero heap allocations in the hot path.
package memtable

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// ArenaBlockSize is the size of each arena block (64 MB).
// Large enough to amortize grow() calls, small enough for tests.
const ArenaBlockSize = 64 * 1024 * 1024 // 64 MB

// arenaAlignment is the required alignment for atomic operations on pointers.
// atomic.Pointer requires 8-byte alignment on all supported platforms.
const arenaAlignment = 8

// Arena is a linear, grow-only allocator.
// Allocations are lock-free in the common case (CAS on head).
// Only block expansion acquires a mutex.
type Arena struct {
	blocks   [][]byte   // slice of blocks, grows on demand
	mu       sync.Mutex // only for expanding blocks, never for allocation
	head     uint64     // atomic offset within the CURRENT block
	blockIdx int        // index of the current block (protected by mu during grow)
}

// NewArena creates a new arena with one pre-allocated block.
func NewArena() *Arena {
	a := &Arena{
		blocks:   make([][]byte, 0, 4), // small initial capacity
		blockIdx: 0,
		head:     0,
	}
	// Pre-allocate the first block — zeroed by make()
	a.blocks = append(a.blocks, make([]byte, ArenaBlockSize))
	return a
}

// Alloc reserves a contiguous region of the given size in the arena.
// Returns a pointer to the reserved memory.
//
// Hot path:
//  1. atomic.LoadUint64(&head) — no mutex
//  2. mu.Lock() — read blockIdx and blocks
//  3. mu.Unlock()
//  4. CAS on head — no mutex
//
// Slow path (grow):
//  1. mu.Lock()
//  2. Create new block
//  3. Update blockIdx
//  4. atomic.StoreUint64(&head, 0)
//  5. mu.Unlock()
//
// Zero allocations in hot path.
// See: ARCH-11, HOT-05, BL-02
//
//go:nosplit
func (a *Arena) Alloc(size int) unsafe.Pointer {
	// Align size to 8 bytes for atomic.Pointer safety.
	// atomic.Pointer requires 8-byte alignment; without this,
	// checkptr panics on misaligned atomic operations.
	// See: checkptr: misaligned pointer conversion
	alignedSize := (size + arenaAlignment - 1) & ^(arenaAlignment - 1)

	for {
		// Step 1: Read head atomically (no mutex)
		currentHead := atomic.LoadUint64(&a.head)

		// Step 2: Read current block under mutex
		a.mu.Lock()
		blockIdx := a.blockIdx
		if blockIdx >= len(a.blocks) {
			a.mu.Unlock()
			a.grow(alignedSize)
			continue
		}
		block := a.blocks[blockIdx]
		blockLen := uint64(len(block))
		a.mu.Unlock()

		// Step 3: Check if allocation fits
		if currentHead+uint64(alignedSize) <= blockLen {
			// Step 4: CAS-reserve the space (no mutex)
			if atomic.CompareAndSwapUint64(&a.head, currentHead, currentHead+uint64(alignedSize)) {
				return unsafe.Pointer(&block[currentHead])
			}
			// CAS failed — another thread reserved space, retry
			continue
		}

		// Step 5: Not enough space — grow (under mutex)
		a.grow(alignedSize)
	}
}

// grow adds a new block to the arena.
// Must be called only when the current block is full.
// Double-checked locking: after acquiring the mutex, verify that
// another thread didn't already grow.
func (a *Arena) grow(size int) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check: another thread might have grown while we waited
	currentHead := atomic.LoadUint64(&a.head)
	if currentHead+uint64(size) <= uint64(len(a.blocks[a.blockIdx])) {
		return // enough space now, another thread already grew
	}

	// Create a new block and add it
	newBlock := make([]byte, ArenaBlockSize)
	a.blocks = append(a.blocks, newBlock)
	a.blockIdx = len(a.blocks) - 1

	// Reset head atomically.
	// Safe: all readers saw the previous block as full and will retry the CAS loop.
	atomic.StoreUint64(&a.head, 0)
}

// Reset clears the arena for reuse.
// After Reset, all previously allocated pointers become invalid.
func (a *Arena) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Keep only the first block, reset head
	a.blocks = a.blocks[:1]
	a.blockIdx = 0
	atomic.StoreUint64(&a.head, 0)

	// Zero the first block to avoid leaking data between resets
	block := a.blocks[0]
	for i := range block {
		block[i] = 0
	}
}

// Size returns the total allocated size across all blocks.
func (a *Arena) Size() uint64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	var total uint64
	for i := 0; i < len(a.blocks)-1; i++ {
		total += uint64(len(a.blocks[i]))
	}
	// Last block: only the used portion
	total += atomic.LoadUint64(&a.head)
	return total
}

// NumBlocks returns the number of blocks allocated.
func (a *Arena) NumBlocks() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.blocks)
}
