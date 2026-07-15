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

// Package memtable provides an in-memory table implementation using a lock-free
// skip list with a linear arena allocator. It supports concurrent reads and writes
// with zero heap allocations in the hot path.
package memtable

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

	// threshold is the probability cutoff for generating a skip list level.
	// Probability 1/4 per level: 0.25 * 0xFFFFFFFF.
	// Computed explicitly so the relationship to the probability is clear.
	threshold = uint32(0.25 * float32(0xFFFFFFFF))
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
	keyOff  uint32                          // смещение от начала узла до байта ключа (0 если нет ключа)
	valOff  uint32                          // смещение от начала узла до байта значения (0 если нет значения)
	keyLen  uint32                          // длина ключа в байтах
	valLen  uint32                          // длина значения в байтах
	ts      uint64                          // инвертированный timestamp (MaxUint64 - commitTS)
	deleted atomic.Bool                     // тумбстоун для удаленных записей
	height  uint32                          // высота узла
	next    [MaxHeight]atomic.Pointer[Node] // фиксированный массив указателей
}

// Key возвращает MVCCKey узла.
// Если keyLen == 0, возвращает MVCCKey{Key: nil} (не пустой срез).
// Создаёт слайс через unsafe.Slice — 0 аллокаций, данные в арене.
//
//go:nosplit
//go:inline
func (n *Node) Key() mvcc.MVCCKey {
	if n.keyLen == 0 {
		return mvcc.MVCCKey{Key: nil, Timestamp: n.ts}
	}
	ptr := unsafe.Add(unsafe.Pointer(n), n.keyOff)
	return mvcc.MVCCKey{
		Key:       unsafe.Slice((*byte)(ptr), int(n.keyLen)),
		Timestamp: n.ts,
	}
}

// Value возвращает значение узла.
// Если valLen == 0, возвращает nil (не пустой срез).
// Создаёт слайс через unsafe.Slice — 0 аллокаций, данные в арене.
//
//go:nosplit
//go:inline
func (n *Node) Value() []byte {
	if n.valLen == 0 {
		return nil
	}
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
	head       *Node
	height     int32
	length     int64
	arena      *Arena
	epoch      *EpochManager        // EBR for safe memory reclamation in lock-free reads
	lastActive atomic.Pointer[Node] // cached last active node for O(1) findLast
	mu         sync.Mutex           // protects write operations (Put, Delete)
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
		epoch:  NewEpochManager(1000), // EBR for safe memory reclamation, cleanup every 1000 retires
	}
}

// NewNode creates a new node in the arena with key and value data
// stored inline in the same allocation.
//
// Total allocation: sizeof(Node) + len(key) + len(value) bytes.
// Key and value are copied directly into the arena — no intermediate buffers.
//
// When key is nil or empty, keyOff stays 0 and Key() returns MVCCKey{Key: nil}.
// When value is nil or empty, valOff stays 0 and Value() returns nil.
//
//go:nosplit
func (a *Arena) NewNode(key, value []byte, height int) *Node {
	keyLen := len(key)
	valLen := len(value)

	// Calculate offsets: Node struct first, then key data, then value data.
	// If keyLen == 0, keyOff stays 0 (no key data stored, Key() returns nil).
	// If valLen == 0, valOff stays 0 (no value data stored, Value() returns nil).
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
	}

	ptr := a.Alloc(offset)
	node := (*Node)(ptr)

	node.keyOff = keyOff
	node.valOff = valOff
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
		keyPtr := unsafe.Add(ptr, keyOff)
		copy(unsafe.Slice((*byte)(keyPtr), keyLen), key)
	}

	// Copy value into arena — direct copy, no intermediate allocation
	if valLen > 0 {
		valPtr := unsafe.Add(ptr, valOff)
		copy(unsafe.Slice((*byte)(valPtr), valLen), value)
	}

	return node
}

// findGreaterOrEqual returns the first node with key >= the given key.
// Lock-free traversal for reads, protected by EBR epoch.
// See: GL-08, ARCH-11
func (s *SkipList) findGreaterOrEqual(key mvcc.MVCCKey) *Node {
	s.epoch.EnterEpoch()

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
			s.epoch.ExitEpoch()
			return next
		}
		level--
	}
	s.epoch.ExitEpoch()
	return nil
}

// findLessOrEqual returns the last node with key <= the given key.
// Lock-free traversal for reads, protected by EBR epoch.
// Returns nil if no node satisfies key <= searchKey.
// See: ARCH-11, MVCC-03
func (s *SkipList) findLessOrEqual(key mvcc.MVCCKey) *Node {
	s.epoch.EnterEpoch()

	x := s.head
	level := atomic.LoadInt32(&s.height) - 1

	for level >= 0 {
		next := x.next[level].Load()
		if next != nil && nodeKeyLess(next.Key(), key) {
			x = next
			continue
		}
		if level == 0 {
			s.epoch.ExitEpoch()
			// At level 0, check if next (not x) satisfies key <= searchKey.
			// x is the last node that was strictly < key.
			// next is the first node >= key.
			// If next != nil and next.Key() <= key (i.e., not strictly greater),
			// return next as the correct "less or equal" result.
			if next != nil {
				nextKey := next.Key()
				// next.Key() <= key means NOT (next.Key() > key)
				// next.Key() > key means nodeKeyLess(key, nextKey) is true
				if !nodeKeyLess(key, nextKey) {
					return next
				}
			}
			if x == s.head {
				return nil
			}
			return x
		}
		level--
	}
	s.epoch.ExitEpoch()
	return nil
}

// findExact returns the node with the exact matching key and timestamp.
// Returns nil if no such node exists.
// Lock-free traversal for reads, protected by EBR epoch.
// See: ARCH-11, MVCC-03
func (s *SkipList) findExact(key mvcc.MVCCKey) *Node {
	node := s.findGreaterOrEqual(key)
	if node == nil {
		return nil
	}
	nodeKey := node.Key()
	if !bytes.Equal(nodeKey.Key, key.Key) {
		return nil
	}
	if nodeKey.Timestamp != key.Timestamp {
		return nil
	}
	return node
}

// findLast returns the last (maximum) node in the skip list.
// Uses cached lastActive pointer for O(1) in the common case.
// Falls back to O(N) traversal when cache is invalidated.
// Protected by EBR epoch for safe lock-free traversal.
// See: GL-08, ARCH-11
func (s *SkipList) findLast() *Node {
	// Fast path: use cached lastActive pointer
	last := s.lastActive.Load()
	if last != nil && !last.deleted.Load() {
		return last
	}

	// Slow path: traverse level 0, skip deleted nodes
	s.epoch.EnterEpoch()

	x := s.head
	level := atomic.LoadInt32(&s.height) - 1
	var lastNode *Node

	for level >= 0 {
		next := x.next[level].Load()
		if next == nil {
			if level == 0 {
				// Return the LAST NON-DELETED node found
				if lastNode != nil {
					s.lastActive.Store(lastNode)
					s.epoch.ExitEpoch()
					return lastNode
				}
				s.epoch.ExitEpoch()
				return x
			}
			level--
		} else {
			x = next
			// Track only non-deleted nodes
			if !x.deleted.Load() {
				lastNode = x
			}
		}
	}
	s.epoch.ExitEpoch()
	if lastNode != nil {
		s.lastActive.Store(lastNode)
	}
	return lastNode
}

// updateLastActive updates the cached lastActive pointer if the given node
// is greater than the current cached value. Called after successful Put.
func (s *SkipList) updateLastActive(node *Node) {
	if node.deleted.Load() {
		return
	}
	for {
		current := s.lastActive.Load()
		if current == nil {
			if s.lastActive.CompareAndSwap(nil, node) {
				return
			}
			continue
		}
		// Check: node > current ?
		// Use nodeKeyLess(current.Key(), node.Key()) to test current < node
		if nodeKeyLess(current.Key(), node.Key()) {
			if s.lastActive.CompareAndSwap(current, node) {
				return
			}
			continue
		}
		// node <= current → no update needed
		return
	}
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

	// Update lastActive cache — the new node might be the new last
	s.updateLastActive(node)

	atomic.AddInt64(&s.length, 1)
	s.mu.Unlock()
	return true
}

// Delete marks a key as deleted (tombstone).
// Uses findExact for precise node matching by key AND timestamp.
// Lock-free write.
func (s *SkipList) Delete(key mvcc.MVCCKey) bool {
	node := s.findExact(key)
	if node == nil {
		return false
	}

	if node.deleted.Load() {
		return false
	}

	node.deleted.Store(true)
	node.valLen = 0
	node.valOff = 0

	// Invalidate lastActive cache if we're deleting the last node
	if s.lastActive.Load() == node {
		s.lastActive.Store(nil)
	}

	s.epoch.Retire(unsafe.Pointer(node)) // EBR: safe deferred reclamation
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
