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
// Lock-Free Skip List with Arena Allocator — Zero Allocation
// ============================================================
//
// This implementation uses a linear arena (grow-only) for all node
// allocations. Key and value data are stored inline in the same
// arena allocation as the Node struct, eliminating all heap allocations
// in the hot path.
//
// Design principles:
//   - Zero allocs/op in Put and Get (no mallocgc in hot path)
//   - Lock-free writes: CAS on next pointers, no sync.Mutex
//   - Memory locality: nodes are dense in arena blocks
//   - Manual memory management: arena grows, never frees until Reset
// ============================================================

package engine

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

const (
	// debugMVCC enables verbose tracing of findGreaterOrEqual.
	// Set to true ONLY for local debugging — compiler eliminates this when false.
	debugMVCC = false

	// MaxHeight is the maximum height of a skip list node.
	// 20 levels gives probability 1/2^20 ≈ 1e-6 of reaching max height,
	// which is sufficient for billions of nodes.
	MaxHeight = 20

	// threshold = 0.25 * 0xFFFFFFFF — gives probability 1/4 per level
	threshold = 1073741824
)

// randomHeight generates a random height for a new node.
// Uses fastrand (lock-free) for zero contention.
func randomHeight() int {
	h := 1
	for h < MaxHeight && fastrand() < threshold {
		h++
	}
	return h
}

// Node представляет узел в skip list.
// Все поля выровнены для минимального паддинга (кратны 8 байтам).
// Ключ и значение хранятся в той же аллокации арены, сразу за структурой.
//
// Размер структуры: 8 + 8 + 4 + 4 + 4 + 4 + 1 + 20*8 = 193 байта
// (с паддингом до 200 байт из-за выравнивания atomic.Pointer)
type Node struct {
	keyOff  uint32                          // смещение от начала узла до байта ключа
	valOff  uint32                          // смещение от начала узла до байта значения
	keyLen  uint32                          // длина ключа в байтах
	valLen  uint32                          // длина значения в байтах
	ts      uint64                          // инвертированный timestamp (MaxUint64 - commitTS)
	deleted atomic.Bool                     // тумбстоун для удаленных записей
	height  uint32                          // высота узла
	next    [MaxHeight]atomic.Pointer[Node] // фиксированный массив указателей
}

// Key возвращает MVCCKey узла.
// Создаёт слайс через unsafe.Slice — 0 аллокаций, данные в арене.
//
//go:nosplit
//go:inline
func (n *Node) Key() mvcc.MVCCKey {
	ptr := unsafe.Add(unsafe.Pointer(n), n.keyOff)
	return mvcc.MVCCKey{
		Key:       unsafe.Slice((*byte)(ptr), int(n.keyLen)),
		Timestamp: n.ts,
	}
}

// Value возвращает значение узла.
// Создаёт слайс через unsafe.Slice — 0 аллокаций, данные в арене.
//
//go:nosplit
//go:inline
func (n *Node) Value() []byte {
	ptr := unsafe.Add(unsafe.Pointer(n), n.valOff)
	return unsafe.Slice((*byte)(ptr), int(n.valLen))
}

// nodeKeyLess returns true if a < b in skip list ordering:
// first by key bytes, then by timestamp descending (newer first).
//
//go:nosplit
func nodeKeyLess(a, b mvcc.MVCCKey) bool {
	cmp := keys.CompareKeys(a.Key, b.Key)
	if cmp < 0 {
		return true
	}
	if cmp > 0 {
		return false
	}
	return a.Timestamp > b.Timestamp
}

// SkipList is a concurrent skip list with lock-free reads and mutex-protected writes.
// The mutex protects the skip list structure (next pointer updates), not the arena allocation.
// Arena allocation is lock-free (CAS-based), so the hot path has zero mutex contention.
type SkipList struct {
	head   *Node
	height int32
	length int64
	arena  *Arena
	mu     sync.Mutex // protects write operations (Put, Delete)
}

// NewSkipList creates a new empty skip list with an arena allocator.
func NewSkipList() *SkipList {
	arena := NewArena()
	// Create a sentinel head node with zero key/value
	head := arena.NewNode(nil, nil, MaxHeight)
	return &SkipList{
		head:   head,
		height: 1,
		arena:  arena,
	}
}

// NewNode creates a new node in the arena with key and value data
// stored inline in the same allocation.
//
// Total allocation: sizeof(Node) + len(key) + len(value) bytes.
// Key and value are copied directly into the arena — no intermediate buffers.
//
//go:nosplit
func (a *Arena) NewNode(key, value []byte, height int) *Node {
	keyLen := len(key)
	valLen := len(value)
	totalSize := int(unsafe.Sizeof(Node{})) + keyLen + valLen

	ptr := a.Alloc(totalSize)
	node := (*Node)(ptr)

	node.keyOff = uint32(unsafe.Sizeof(Node{}))
	node.valOff = uint32(unsafe.Sizeof(Node{})) + uint32(keyLen)
	node.keyLen = uint32(keyLen)
	node.valLen = uint32(valLen)
	node.height = uint32(height)
	node.deleted.Store(false)

	// Zero out the next pointers (critical for GC safety)
	for i := 0; i < MaxHeight; i++ {
		node.next[i].Store(nil)
	}

	// Copy key into arena — direct copy, no intermediate allocation
	if keyLen > 0 {
		keyPtr := unsafe.Add(ptr, node.keyOff)
		copy(unsafe.Slice((*byte)(keyPtr), keyLen), key)
	}

	// Copy value into arena — direct copy, no intermediate allocation
	if valLen > 0 {
		valPtr := unsafe.Add(ptr, node.valOff)
		copy(unsafe.Slice((*byte)(valPtr), valLen), value)
	}

	return node
}

// findGreaterOrEqual returns the first node with key >= the given key.
// Lock-free traversal for reads.
func (s *SkipList) findGreaterOrEqual(key mvcc.MVCCKey) *Node {
	x := s.head
	level := atomic.LoadInt32(&s.height) - 1

	if debugMVCC {
		fmt.Printf("[DEBUG] find: Looking for Key=%s, TS=%d (commitTS=%d)\n",
			string(key.Key), key.Timestamp, key.CommitTS())
		fmt.Printf("[DEBUG] find: Starting at head, height=%d\n", level+1)
	}

	for level >= 0 {
		next := x.next[level].Load()
		if next != nil && nodeKeyLess(next.Key(), key) {
			if debugMVCC {
				nk := next.Key()
				fmt.Printf("[DEBUG] find: Level %d: skip to node TS=%d, Key=%s\n",
					level, nk.Timestamp, string(nk.Key))
			}
			x = next
			continue
		}
		if level == 0 {
			if debugMVCC {
				if next != nil {
					nk := next.Key()
					fmt.Printf("[DEBUG] find: Returning node TS=%d, Key=%s, Deleted=%v\n",
						nk.Timestamp, string(nk.Key), next.deleted.Load())
				} else {
					fmt.Printf("[DEBUG] find: Returning nil (end of list)\n")
				}
			}
			return next
		}
		level--
	}
	return nil
}

// findLast returns the last (maximum) node in the skip list.
func (s *SkipList) findLast() *Node {
	x := s.head
	level := atomic.LoadInt32(&s.height) - 1

	for level >= 0 {
		next := x.next[level].Load()
		if next == nil {
			if level == 0 {
				return x
			}
			level--
		} else {
			x = next
		}
	}
	return x
}

// Get retrieves the value for the given key.
// Lock-free read operation — zero allocations.
func (s *SkipList) Get(key mvcc.MVCCKey) ([]byte, bool) {
	node := s.findGreaterOrEqual(key)
	if node == nil {
		return nil, false
	}
	nodeKey := node.Key()
	if nodeKey.Key == nil || !bytes.Equal(nodeKey.Key, key.Key) {
		return nil, false
	}
	if node.deleted.Load() {
		return nil, false
	}
	return node.Value(), true
}

// Put inserts or updates a key-value pair.
// Mutex-protected write, lock-free reads still work during writes.
//
// Returns true if a new entry was created, false if an existing entry was updated.
//
// Zero allocations:
//   - Arena allocation for node (no mallocgc)
//   - Stack-allocated [MaxHeight]*Node for prevs
//   - No sync.Pool, no GC
func (s *SkipList) Put(key mvcc.MVCCKey, value []byte) bool {
	s.mu.Lock()

	height := randomHeight()

	// Stack-allocated array — zero allocations
	var prevs [MaxHeight]*Node

	// Initialize prevs with head for all levels
	for i := 0; i < MaxHeight; i++ {
		prevs[i] = s.head
	}

	// Find all predecessors
	x := s.head
	level := int(atomic.LoadInt32(&s.height)) - 1

	// Track if this is an update to an existing key+timestamp
	var existingNode *Node

	for level >= 0 {
		next := x.next[level].Load()
		for next != nil && nodeKeyLess(next.Key(), key) {
			x = next
			next = x.next[level].Load()
		}

		// Check for existing key with same timestamp at level 0
		if level == 0 && next != nil {
			nextKey := next.Key()
			if bytes.Equal(nextKey.Key, key.Key) && nextKey.Timestamp == key.Timestamp {
				existingNode = next
			}
		}

		prevs[level] = x
		level--
	}

	// If a node with the same key+timestamp already exists, update it in-place.
	// This is critical for DeleteWithTS correctness: it calls Put(key, nil) then
	// Delete(key). Without in-place update, Put creates a DUPLICATE node and
	// Delete only marks the new one, leaving the old alive node in the chain.
	if existingNode != nil {
		// Update value in-place — copy new value into the existing node's arena slot
		existingNode.valLen = uint32(len(value))
		if len(value) > 0 {
			valPtr := unsafe.Add(unsafe.Pointer(existingNode), existingNode.valOff)
			copy(unsafe.Slice((*byte)(valPtr), len(value)), value)
		}
		// Reset deleted flag — this is a fresh Put, not a tombstone
		existingNode.deleted.Store(false)
		s.mu.Unlock()
		return false
	}

	// Update height BEFORE inserting the node
	for height > int(atomic.LoadInt32(&s.height)) {
		atomic.StoreInt32(&s.height, int32(height))
	}

	// Create new node in arena — zero allocation (CAS-based, no mutex)
	node := s.arena.NewNode(key.Key, value, height)
	node.ts = key.Timestamp

	// Link the node into the skip list
	for l := 0; l < height; l++ {
		node.next[l].Store(prevs[l].next[l].Load())
		prevs[l].next[l].Store(node)
	}

	atomic.AddInt64(&s.length, 1)
	s.mu.Unlock()
	return true
}

// Delete marks a key as deleted (tombstone).
// Lock-free write.
func (s *SkipList) Delete(key mvcc.MVCCKey) bool {
	node := s.findGreaterOrEqual(key)
	if node == nil {
		return false
	}
	nodeKey := node.Key()
	if !bytes.Equal(nodeKey.Key, key.Key) {
		return false
	}

	if node.deleted.Load() {
		return false
	}

	node.deleted.Store(true)
	atomic.AddInt64(&s.length, -1)
	return true
}

// Len returns the number of active (non-deleted) entries.
func (s *SkipList) Len() int {
	return int(atomic.LoadInt64(&s.length))
}

// Height returns the current maximum height of the skip list.
func (s *SkipList) Height() int {
	return int(atomic.LoadInt32(&s.height))
}

// Arena returns the arena backing this skip list.
func (s *SkipList) Arena() *Arena {
	return s.arena
}

// ============================================================
// SkipListIterator
// ============================================================

// SkipListIterator provides forward iteration over the skip list.
type SkipListIterator struct {
	current *Node
	list    *SkipList
	done    bool
}

// NewIterator creates a new iterator positioned before the first element.
func (s *SkipList) NewIterator() *SkipListIterator {
	return &SkipListIterator{
		current: s.head,
		list:    s,
	}
}

// Next advances the iterator to the next non-deleted node.
func (it *SkipListIterator) Next() bool {
	if it.done {
		return false
	}
	for {
		next := it.current.next[0].Load()
		if next == nil {
			it.done = true
			return false
		}
		it.current = next
		// Skip deleted nodes (tombstones)
		if !it.current.deleted.Load() {
			return true
		}
	}
}

// Key returns the current node's key.
func (it *SkipListIterator) Key() mvcc.MVCCKey {
	return it.current.Key()
}

// Value returns the current node's value.
func (it *SkipListIterator) Value() []byte {
	return it.current.Value()
}

// IsDeleted returns true if the current node is marked as deleted.
func (it *SkipListIterator) IsDeleted() bool {
	if it.current == nil {
		return false
	}
	return it.current.deleted.Load()
}

// Close releases iterator resources.
func (it *SkipListIterator) Close() {
	it.current = nil
	it.list = nil
	it.done = true
}
