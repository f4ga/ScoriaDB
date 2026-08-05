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

package memtable

import (
	"bytes"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

const (
	// MaxHeight is the maximum height of a skip list node.
	// 20 levels gives probability 1/2^20 ≈ 1e-6 of reaching max height,
	// which is sufficient for billions of nodes.
	MaxHeight = 20

	// threshold is the probability cutoff for generating a skip list level.
	// Probability 1/4 per level: 0.25 * 0xFFFFFFFF.
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

// Node represents a node in the skip list.
// All fields are aligned for minimal padding (multiples of 8 bytes).
// Key and value are stored inline in the same arena allocation, immediately
// after the Node struct.
//
// keyOff: offset from node start to key data (0 if no key)
// valOff: offset from node start to value data
//   - valOff == 0 AND valLen == 0 → nil value (tombstone)
//   - valOff > 0 AND valLen == 0 → empty value ([]byte{})
//   - valOff > 0 AND valLen > 0 → actual value
//
// This distinction is critical for MVCC semantics: nil is a tombstone,
// empty slice is a valid value.
type Node struct {
	keyOff  uint32                          // offset to key data (0 if no key)
	valOff  uint32                          // offset to value data (0 if nil value)
	keyLen  uint32                          // key length in bytes
	valLen  uint32                          // value length in bytes (0 for nil OR empty)
	ts      uint64                          // inverted timestamp (MaxUint64 - commitTS)
	deleted atomic.Bool                     // tombstone flag for deleted entries
	height  atomic.Uint32                   // node height (atomic for lock-free reads)
	next    [MaxHeight]atomic.Pointer[Node] // fixed array of next pointers
}

// Key returns the MVCCKey of the node.
// If keyLen == 0, returns MVCCKey{Key: nil} (not empty slice).
// Zero allocations — data is in arena.
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

// Value returns the node's value.
// CRITICAL: Distinguishes between nil and empty slice:
//   - valOff == 0 → nil (tombstone)
//   - valOff > 0 && valLen == 0 → []byte{} (empty value)
//   - valOff > 0 && valLen > 0 → actual value
//
// Zero allocations — data is in arena.
// See: BL-20 (TECHDEBT)
//
//go:nosplit
//go:inline
func (n *Node) Value() []byte {
	if n.deleted.Load() {
		return nil
	}
	if n.valOff == 0 {
		return nil // genuine nil value (tombstone)
	}
	if n.valLen == 0 {
		return []byte{} // empty but non-nil value
	}
	ptr := unsafe.Add(unsafe.Pointer(n), n.valOff)
	return unsafe.Slice((*byte)(ptr), int(n.valLen))
}

// IsDeleted returns true if the node is marked as deleted.
// Used for MVCC filtering.
//
//go:nosplit
//go:inline
func (n *Node) IsDeleted() bool {
	return n.deleted.Load()
}

// nodeKeyLess returns true if a < b in skip list ordering:
// first by user key (bytes.Compare), then by timestamp descending (newer first).
//
// This ordering ensures that for the same user key, the newest version
// (largest commitTS, smallest inverted timestamp) appears first.
// See: ARCH-07 (MVCC with inverted timestamp)
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
	// Same user key → newer (larger commitTS) comes first
	// commitTS is inverted: larger commitTS = smaller Timestamp
	// So a.Timestamp > b.Timestamp means a is OLDER (smaller commitTS)
	// Wait: In MVCC, we want newer versions first.
	// With inverted timestamps, smaller Timestamp = newer.
	// So nodeKeyLess should return true if a is NEWER than b?
	// Actually: we want ascending order by commitTS (oldest first).
	// So smaller commitTS = larger Timestamp = comes first.
	// Therefore: a.Timestamp > b.Timestamp → a is older → a < b.
	return a.Timestamp > b.Timestamp
}

// SkipList is a concurrent skip list with lock-free reads and mutex-protected writes.
// The mutex protects the skip list structure (next pointer updates), not the arena.
// Arena allocation is lock-free (CAS-based), so the hot path has zero contention.
// Lock-free writes (CAS-based) are planned for v0.4.0.
type SkipList struct {
	head       *Node
	height     int32
	length     int64
	arena      *Arena
	epoch      *EpochManager        // EBR for safe memory reclamation
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
		epoch:  NewEpochManager(1000),
	}
}

// NewNode creates a new node in the arena with key and value data
// stored inline in the same allocation.
//
// Total allocation: sizeof(Node) + len(key) + len(value) bytes.
// Key and value are copied directly into the arena — no intermediate buffers.
//
// Key handling:
//   - key == nil or len(key) == 0 → keyOff = 0, Key() returns nil
//
// Value handling (CRITICAL for MVCC):
//   - value == nil → valOff = 0, Value() returns nil (tombstone)
//   - value != nil && len(value) == 0 → valOff = sentinel (1 byte), Value() returns []byte{}
//   - value != nil && len(value) > 0 → valOff = offset, Value() returns actual data
//
// See: BL-20 (TECHDEBT)
//
//go:nosplit
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
		// Empty but non-nil value → allocate 1 byte as sentinel.
		// This allows Value() to distinguish nil (valOff=0) from empty (valOff>0, valLen=0).
		valOff = uint32(offset)
		offset += 1
	}
	// If value == nil, valOff stays 0 → Value() returns nil.

	ptr := a.Alloc(offset)
	node := (*Node)(ptr)

	node.keyOff = keyOff
	node.valOff = valOff
	node.keyLen = uint32(keyLen)
	node.valLen = uint32(valLen)
	node.height.Store(uint32(height))
	node.deleted.Store(false)

	// Zero out next pointers (critical for GC safety)
	for i := 0; i < MaxHeight; i++ {
		node.next[i].Store(nil)
	}

	// Copy key into arena — direct copy, no allocation
	if keyLen > 0 {
		keyPtr := unsafe.Add(ptr, keyOff)
		copy(unsafe.Slice((*byte)(keyPtr), keyLen), key)
	}

	// Copy value into arena — direct copy, no allocation
	if valLen > 0 {
		valPtr := unsafe.Add(ptr, valOff)
		copy(unsafe.Slice((*byte)(valPtr), valLen), value)
	}
	// If valOff > 0 && valLen == 0 (sentinel for empty value), we allocated 1 byte but don't copy anything.
	// The sentinel byte is left as zero.

	return node
}

// findGreaterOrEqual returns the first node with key >= the given key.
// Lock-free traversal for reads, protected by EBR epoch.
// See: GL-08, ARCH-11
func (s *SkipList) findGreaterOrEqual(key mvcc.MVCCKey) *Node {
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
			return next
		}
		level--
	}
	s.epoch.ExitEpoch()
	return nil
}

// findExact returns the node with exact key+timestamp match.
// Returns nil if no such node exists.
// Lock-free traversal, protected by EBR epoch.
// See: ARCH-11, MVCC-03
func (s *SkipList) findExact(key mvcc.MVCCKey) *Node {
	node := s.findGreaterOrEqual(key)
	if node == nil {
		return nil
	}
	nodeKey := node.Key()
	if nodeKey.Key == nil || !bytes.Equal(nodeKey.Key, key.Key) {
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
	// Fast path: cached lastActive
	last := s.lastActive.Load()
	if last != nil && !last.deleted.Load() {
		return last
	}

	// Slow path: traverse LEVEL 0 ONLY, skip deleted nodes.
	// Level 0 contains ALL nodes, guaranteeing complete traversal.
	s.epoch.EnterEpoch()
	defer s.epoch.ExitEpoch()

	x := s.head
	var lastNonDeleted *Node

	for {
		next := x.next[0].Load()
		if next == nil {
			break
		}
		if !next.deleted.Load() {
			lastNonDeleted = next
		}
		x = next
	}

	if lastNonDeleted != nil {
		s.lastActive.Store(lastNonDeleted)
		return lastNonDeleted
	}
	return s.head
}

// updateLastActive updates the cached lastActive pointer if the given node
// is greater than the current cached value. Called after successful Put.
//
// Comparison for "greater": first by user key, then by timestamp.
// Since timestamps are inverted, a "greater" node has:
//   - larger user key, OR
//   - same user key and smaller Timestamp (newer version)
//
// See: ARCH-11
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
//
// Returns:
//   - (value, true)  — key exists with value (may be []byte{} for empty value)
//   - (nil, false)   — key does not exist OR was deleted (tombstone)
//
// Performance: O(log N), zero allocations, no mutex.
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
// Value handling (CRITICAL for MVCC):
//   - value == nil     → tombstone (valOff=0, valLen=0)
//   - value == []byte{} → empty value (valOff=sentinel, valLen=0, deleted=false)
//   - value != nil     → normal value (valOff=offset, valLen=len(value))
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

	// If a node with the same key+timestamp already exists, return false without
	// modifying it. In LSM architecture each record has a unique timestamp, so
	// this case should not occur. In-place updates of valOff/valLen would race
	// with lock-free Value() reads, so we avoid them entirely.
	if existingNode != nil {
		s.mu.Unlock()
		return false
	}
	// Update height BEFORE inserting the node
	for height > int(atomic.LoadInt32(&s.height)) {
		atomic.StoreInt32(&s.height, int32(height))
	}

	// Create new node in arena — zero allocation
	// Arena.NewNode handles nil vs empty correctly:
	//   - value == nil     → valOff=0, valLen=0
	//   - value == []byte{} → valOff=sentinel, valLen=0
	//   - value != nil     → valOff=offset, valLen=len(value)
	node := s.arena.NewNode(key.Key, value, height)
	node.ts = key.Timestamp

	// Link the node into the skip list
	for l := 0; l < height; l++ {
		node.next[l].Store(prevs[l].next[l].Load())
		prevs[l].next[l].Store(node)
	}

	// Update lastActive cache
	s.updateLastActive(node)

	atomic.AddInt64(&s.length, 1)
	s.mu.Unlock()
	return true
}

// Delete marks a key as deleted (tombstone).
// Uses findExact for precise node matching by key AND timestamp.
//
// CRITICAL: Does NOT modify valOff/valLen — only sets the deleted flag.
// valOff/valLen are read lock-free by Value() and must not be mutated
// after node creation. The deleted flag (atomic.Bool) is sufficient
// to mark a tombstone — all callers check deleted.Load() before Value().
// See: SYMPTOM-01 (data race between Delete and Value)
func (s *SkipList) Delete(key mvcc.MVCCKey) bool {
	s.mu.Lock()

	node := s.findExact(key)
	if node == nil {
		s.mu.Unlock()
		return false
	}

	if node.deleted.Load() {
		s.mu.Unlock()
		return false
	}

	node.deleted.Store(true)

	// Invalidate lastActive cache if we're deleting the last node
	if s.lastActive.Load() == node {
		s.lastActive.Store(nil)
	}

	s.epoch.Retire(unsafe.Pointer(node))
	atomic.AddInt64(&s.length, -1)
	s.mu.Unlock()
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

// getHeight returns the node height atomically.
// Used in lock-free Delete.
func (n *Node) getHeight() int {
	return int(n.height.Load())
}

// findGreaterOrEqualFull returns predecessors and successors for all levels.
// Used for CAS insertion. Lock-free traversal (read-only).
func (s *SkipList) findGreaterOrEqualFull(key mvcc.MVCCKey) (preds, succs [MaxHeight]*Node) {
	x := s.head
	level := int(atomic.LoadInt32(&s.height)) - 1

	for level >= 0 {
		next := x.next[level].Load()
		for next != nil && nodeKeyLess(next.Key(), key) {
			x = next
			next = x.next[level].Load()
		}
		preds[level] = x
		succs[level] = next
		level--
	}
	return preds, succs
}

// findPredecessor returns the predecessor at the given level.
// Used for CAS deletion. Lock-free traversal (read-only).
func (s *SkipList) findPredecessor(key mvcc.MVCCKey, level int) *Node {
	x := s.head
	top := int(atomic.LoadInt32(&s.height)) - 1

	for l := top; l >= level; l-- {
		next := x.next[l].Load()
		for next != nil && nodeKeyLess(next.Key(), key) {
			x = next
			next = x.next[l].Load()
		}
	}
	return x
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
