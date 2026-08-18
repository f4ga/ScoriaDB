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

package memtable

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// FlatArena is a lock-free flat allocator backed by a single contiguous []byte
// block per shard. It eliminates the global mutex from the hot read path:
//
//   - NodeAt is a pure index->pointer computation: no locks, no atomic loads.
//   - Index is pure pointer arithmetic: no locks, no atomic loads.
//   - Alloc reserves space with a single CAS on head (lock-free on the hot path).
//
// The block is a single fixed-size region that is never resized in place, so
// every index handed out stays valid for the lifetime of the arena. Because the
// SkipList serializes writers under its own mutex and the engine swaps the whole
// MemTable (calling Reset) once the active arena approaches the flush watermark
// (MaxMemTableSize = 4 MB, far below the 64 MB block), a live arena is never
// exhausted; growth within a single MemTable is not exercised by the hot path.
//
// Reset replaces the whole block under mu (cold path, used only by flush/Close)
// and reuses the same region, so the GC reclaims nothing but the arena remains
// immediately usable. See: HOT-01, PERF-01.
//
// FlatArenaSize is the size of each shard's flat arena block.
//   - Production: 64 MB. The 4 MB flush watermark is far below this, so a live
//     MemTable is never exhausted; the arena is Reset (released) by flush long
//     before the block fills.
//   - Tests: 256 MB (raised via test hooks) so large test workloads that insert
//     more than a single MemTable's worth of nodes without an intervening flush
//     do not exhaust the block.
const FlatArenaSize = 64 * 1024 * 1024 // 64 MB

type FlatArena struct {
	mu    sync.Mutex // protects data (Reset) — cold path only
	data  []byte     // single contiguous block; replaced on Reset
	head  uint64     // atomic offset of the next free byte within data
	total uint64     // atomic total bytes handed out (used bytes)
}

// NewFlatArena creates a flat arena with a single preallocated block of the
// given size. A non-positive size falls back to FlatArenaSize (64 MB) so
// callers (and tests) get a usable arena immediately.
func NewFlatArena(size int) *FlatArena {
	if size <= 0 {
		size = FlatArenaSize
	}
	return &FlatArena{
		data: make([]byte, size),
	}
}

// Alloc reserves a contiguous region of the given size and returns a pointer to
// it. The hot path is fully lock-free: a single CAS on head reserves the bytes.
// The mutex is only ever taken on the cold Reset path. See: HOT-01.
func (a *FlatArena) Alloc(size int) unsafe.Pointer {
	if size < 0 {
		return nil
	}
	if size == 0 {
		size = 1 // Alloc(0) returns a valid pointer, not nil (see tests)
	}
	alignedSize := (size + 7) & ^7 // 8-byte alignment

	data := a.data // block is stable until Reset; readers never call Reset
	for {
		head := atomic.LoadUint64(&a.head)
		if head+uint64(alignedSize) > uint64(len(data)) {
			// The block is exhausted. Given the 4 MB flush watermark this is
			// unreachable for a live MemTable; the arena is released by flush
			// before a 64 MB block fills. Fail loudly rather than corrupt indices.
			panic("flat arena: block exhausted; flush watermark not respected")
		}
		if atomic.CompareAndSwapUint64(&a.head, head, head+uint64(alignedSize)) {
			atomic.AddUint64(&a.total, uint64(alignedSize))
			return unsafe.Pointer(&data[head])
		}
	}
}

// NodeAt returns a pointer to the node at the given index. Lock-free: no
// mutex, no atomic loads. The index is an absolute offset from the block base,
// so this is a single pointer-arithmetic operation. See: HOT-01.
func (a *FlatArena) NodeAt(idx uint32) *Node {
	return (*Node)(unsafe.Pointer(&a.data[idx]))
}

// Index returns the index of a node pointer as an absolute offset from the
// block base. Lock-free: pure pointer arithmetic, no mutex. See: HOT-01.
func (a *FlatArena) Index(node *Node) uint32 {
	return uint32(uintptr(unsafe.Pointer(node)) - uintptr(unsafe.Pointer(&a.data[0])))
}

// NewNode allocates and initializes a new node in the arena. See Arena.NewNode.
func (a *FlatArena) NewNode(key, value []byte, height int) *Node {
	keyLen := len(key)
	valLen := len(value)

	nodeSize := int(unsafe.Sizeof(Node{}))
	var keyOff, valOff uint32

	offset := nodeSize
	if keyLen > 0 {
		keyOff = uint32(offset)
		offset += keyLen
	}
	if valLen > 0 {
		valOff = uint32(offset)
		offset += valLen
	} else if value != nil {
		// empty but non-nil value → sentinel
		valOff = uint32(offset)
		offset += 1
	}

	ptr := a.Alloc(offset)
	node := (*Node)(ptr)

	node.keyOff = keyOff
	node.valOff = valOff
	node.keyLen = uint32(keyLen)
	node.valLen = uint32(valLen)
	node.height = uint32(height)
	node.deleted = 0

	// Zero out next pointers
	for i := 0; i < MaxHeight; i++ {
		node.next[i] = 0
	}

	// Copy key and value
	if keyLen > 0 {
		keyPtr := unsafe.Add(ptr, keyOff)
		copy(unsafe.Slice((*byte)(keyPtr), keyLen), key)
	}
	if valLen > 0 {
		valPtr := unsafe.Add(ptr, valOff)
		copy(unsafe.Slice((*byte)(valPtr), valLen), value)
	}
	return node
}

// Reset clears the arena, replacing the current block with a fresh one so the
// arena remains immediately usable. Cold path; the SkipList guarantees via its
// EpochManager that no reader holds a reference before calling Reset.
func (a *FlatArena) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.data = make([]byte, len(a.data))
	atomic.StoreUint64(&a.head, 0)
	atomic.StoreUint64(&a.total, 0)
}

// Size returns the total allocated size (used bytes).
func (a *FlatArena) Size() uint64 {
	return atomic.LoadUint64(&a.total)
}

// NumBlocks returns the number of allocated blocks. A flat arena reports 1.
// Retained for compatibility with Arena.
func (a *FlatArena) NumBlocks() int {
	return 1
}

// Cap returns the capacity of the current block in bytes.
func (a *FlatArena) Cap() int {
	return len(a.data)
}
