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

const (
	// MaxHeight is the maximum height of a skip list node.
	MaxHeight = 20
	// threshold is the probability cutoff for generating a skip list level.
	threshold = uint32(0.25 * float32(0xFFFFFFFF))
	// ArenaBlockSize is the size of a single grow-only arena block.
	// 4 MB matches the MemTable flush watermark (MaxMemTableSize), so a flush
	// releases the arena's allocated blocks promptly instead of holding on to
	// hundreds of MB (previously 512 MB per block caused OOM across 16 shards).
	// See: ARCH-07, PERF-01, SYMPTOM-03
	ArenaBlockSize = 4 * 1024 * 1024
	// MaxArenaBlocks bounds the number of blocks a single arena may allocate.
	// 4 MB x 256 = 1 GB per shard is a safe ceiling that still allows a heavy
	// write burst before flush catches up, without OOM'ing the process.
	MaxArenaBlocks = 256
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

// randomHeight generates a random height for a new node.
func randomHeight() int {
	h := 1
	for h < MaxHeight && fastrand() < threshold {
		h++
	}
	return h
}

// Node represents a node in the skip list.
// All fields are stored inline in the arena.
type Node struct {
	keyOff  uint32
	valOff  uint32
	keyLen  uint32
	valLen  uint32
	ts      uint64
	deleted uint32 // 0 = active, 1 = tombstone
	height  uint32
	next    [MaxHeight]uint32 // indices into arena
}

// Arena is a grow-only allocator backed by a list of fixed-size blocks.
//
// The first block is allocated lazily on the first Alloc call. Each shard's
// MemTable therefore reserves NO memory until it actually stores data. This
// avoids the per-shard preallocation blowup (up to 16×512 MB on a large core
// count machine) that previously OOM'd the test process during benchmarks.
//
// Blocks are allocated and appended under mu; within a block the head offset
// is advanced with a CAS, so Alloc is lock-free on the hot path once a block
// exists. Node indices are computed relative to block 0 so all indices remain
// valid regardless of which block holds the node.
type Arena struct {
	mu       sync.Mutex // protects blocks, blockIdx
	blocks   [][]byte
	blockIdx int    // index of the current (active) block
	head     uint64 // atomic offset within the current block
	total    uint64 // atomic total bytes allocated
}

// NewArena creates a new arena. No memory is allocated until the first Alloc.
func NewArena() *Arena {
	return &Arena{}
}

// Alloc reserves a contiguous region of the given size.
// Returns a pointer to the reserved memory.
//
// Hot path is lock-free (CAS on head) once a block exists; the mutex is taken
// only when growing the block list (cold path, ~1 per 4 MB).
func (a *Arena) Alloc(size int) unsafe.Pointer {
	if size <= 0 {
		return nil
	}
	alignedSize := (size + 7) & ^7 // 8-byte alignment

	// Fast path: try the current block with CAS on head.
	// blockIdx is only mutated under mu during growth, so we read it under mu
	// to avoid a torn index. If no block exists yet, lazily create the first one
	// (avoids reserving ArenaBlockSize per MemTable at construction time).
	a.mu.Lock()
	if len(a.blocks) == 0 {
		a.blocks = append(a.blocks, make([]byte, ArenaBlockSize))
		a.blockIdx = 0
	}
	blockIdx := a.blockIdx
	block := a.blocks[blockIdx]
	a.mu.Unlock()

	for {
		currentHead := atomic.LoadUint64(&a.head)
		if currentHead+uint64(alignedSize) <= uint64(len(block)) {
			if atomic.CompareAndSwapUint64(&a.head, currentHead, currentHead+uint64(alignedSize)) {
				atomic.AddUint64(&a.total, uint64(alignedSize))
				return unsafe.Pointer(&block[currentHead])
			}
			continue
		}
		// Current block full — grow on the cold path.
		return a.grow(alignedSize)
	}
}

// grow appends a new block and retries the allocation. Cold path.
func (a *Arena) grow(size int) unsafe.Pointer {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Re-check: another goroutine may have grown the block list already.
	block := a.blocks[a.blockIdx]
	currentHead := atomic.LoadUint64(&a.head)
	if currentHead+uint64(size) <= uint64(len(block)) {
		if atomic.CompareAndSwapUint64(&a.head, currentHead, currentHead+uint64(size)) {
			atomic.AddUint64(&a.total, uint64(size))
			return unsafe.Pointer(&block[currentHead])
		}
	}

	// Never panic or reset the block list: an arena is grow-only and all live
	// nodes reference indices that stay valid only if previous blocks remain
	// reachable. Resetting a.blocks while the SkipList still references old
	// indices makes every existing node dangling (NodeAt returns nil), which
	// SIGSEGVs the hot write path. Instead we always append a fresh block; the
	// MemTable releases the whole arena via Close()/Reset() once flushed, so
	// memory is reclaimed by the normal flush cycle. MaxArenaBlocks remains a
	// soft guidance, not a hard reset trigger. See: DEF-12, SYMPTOM-03
	newBlock := make([]byte, ArenaBlockSize)
	a.blocks = append(a.blocks, newBlock)
	a.blockIdx = len(a.blocks) - 1
	// Reset head for the new block.
	atomic.StoreUint64(&a.head, 0)

	block = newBlock
	currentHead = 0
	if currentHead+uint64(size) <= uint64(len(block)) {
		atomic.StoreUint64(&a.head, uint64(size))
		atomic.AddUint64(&a.total, uint64(size))
		return unsafe.Pointer(&block[currentHead])
	}
	panic("arena: block too small for allocation")
}

// NewNode allocates and initializes a new node in the arena.
func (a *Arena) NewNode(key, value []byte, height int) *Node {
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

// NodeAt returns a pointer to the node at the given index.
// Indices are computed relative to the start of block 0, so a node can live in
// any block. The block index is derived by dividing the index by the block size.
func (a *Arena) NodeAt(idx uint32) *Node {
	a.mu.Lock()
	blockIdx := idx / ArenaBlockSize
	if int(blockIdx) >= len(a.blocks) {
		a.mu.Unlock()
		return nil
	}
	block := a.blocks[blockIdx]
	a.mu.Unlock()
	return (*Node)(unsafe.Pointer(&block[idx%ArenaBlockSize]))
}

// Index returns the index of a node pointer relative to the start of block 0.
// This index is what NodeAt() expects to reconstruct the node.
func (a *Arena) Index(node *Node) uint32 {
	a.mu.Lock()
	defer a.mu.Unlock()
	for bi, block := range a.blocks {
		base := uintptr(unsafe.Pointer(&block[0]))
		p := uintptr(unsafe.Pointer(node))
		if p >= base && p < base+uintptr(len(block)) {
			off := p - base
			return uint32(bi)*ArenaBlockSize + uint32(off)
		}
	}
	return 0
}

// Reset clears the arena, releasing all blocks and reallocating lazily.
func (a *Arena) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocks = nil
	a.blockIdx = 0
	atomic.StoreUint64(&a.head, 0)
	atomic.StoreUint64(&a.total, 0)
}

// Size returns the total allocated size (used bytes).
func (a *Arena) Size() uint64 {
	return atomic.LoadUint64(&a.total)
}

// NumBlocks returns the number of allocated blocks.
func (a *Arena) NumBlocks() int {
	a.mu.Lock()
	n := len(a.blocks)
	a.mu.Unlock()
	return n
}
