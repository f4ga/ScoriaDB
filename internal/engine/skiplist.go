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
// Lock-Free Skip List with EBR (Epoch-Based Reclamation)
// ============================================================

package engine

import (
	"bytes"
	"sync"
	"sync/atomic"
	_ "unsafe" // required for //go:linkname

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

const (
	maxHeight = 32
	// threshold = 0.25 * 0xFFFFFFFF — gives probability 1/4 per level
	threshold = 1073741824
)

func randomHeight() int {
	h := 1
	for h < maxHeight && fastrand() < threshold {
		h++
	}
	return h
}

// Node represents a single node in the lock-free skip list.
type Node struct {
	key     mvcc.MVCCKey
	value   atomic.Pointer[[]byte]
	deleted atomic.Bool
	next    []atomic.Pointer[Node]
	height  int
}

func newNode(key mvcc.MVCCKey, value []byte, height int) *Node {
	n := &Node{
		key:    key,
		height: height,
		next:   make([]atomic.Pointer[Node], height),
	}
	if value != nil {
		n.value.Store(&value)
	}
	return n
}

// nodeKeyLess returns true if a < b in skip list ordering:
// first by key bytes, then by timestamp descending (newer first).
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
type SkipList struct {
	head   *Node
	height int32
	length int64
	mu     sync.Mutex // protects writes (Put, Delete)
}

// NewSkipList creates a new empty skip list.
func NewSkipList() *SkipList {
	head := newNode(mvcc.MVCCKey{}, nil, maxHeight)
	return &SkipList{
		head:   head,
		height: 1,
	}
}

// findGreaterOrEqual returns the first node with key >= the given key.
// Lock-free traversal for reads.
func (s *SkipList) findGreaterOrEqual(key mvcc.MVCCKey) *Node {
	epochManager.EnterEpoch()
	defer epochManager.ExitEpoch()

	x := s.head
	level := atomic.LoadInt32(&s.height) - 1

	for level >= 0 {
		next := x.next[level].Load()
		if next != nil && nodeKeyLess(next.key, key) {
			x = next
			continue
		}
		if level == 0 {
			return next
		}
		level--
	}
	return nil
}

// findLessThan fills prev[] with predecessors for each level.
// Lock-free traversal for reads.
func (s *SkipList) findLessThan(key mvcc.MVCCKey, prev []*Node) *Node {
	epochManager.EnterEpoch()
	defer epochManager.ExitEpoch()

	x := s.head
	level := atomic.LoadInt32(&s.height) - 1

	for level >= 0 {
		next := x.next[level].Load()
		if next != nil && nodeKeyLess(next.key, key) {
			x = next
			continue
		}
		if prev != nil && level < int32(len(prev)) {
			prev[level] = x
		}
		if level == 0 {
			return x
		}
		level--
	}
	return x
}

// findLast returns the last (maximum) node in the skip list.
func (s *SkipList) findLast() *Node {
	epochManager.EnterEpoch()
	defer epochManager.ExitEpoch()

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
// Lock-free read operation.
func (s *SkipList) Get(key mvcc.MVCCKey) ([]byte, bool) {
	node := s.findGreaterOrEqual(key)
	if node == nil || node.key.Key == nil || !bytes.Equal(node.key.Key, key.Key) {
		return nil, false
	}
	if node.deleted.Load() {
		return nil, false
	}
	val := node.value.Load()
	if val == nil {
		return nil, false
	}
	return *val, true
}

// Put inserts or updates a key-value pair.
// Mutex-protected write, lock-free reads still work during writes.
func (s *SkipList) Put(key mvcc.MVCCKey, value []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()

	height := randomHeight()
	prevs := make([]*Node, maxHeight)

	// Initialize prevs with head for all levels to ensure no nil pointers
	// for levels above current list height.
	for i := 0; i < maxHeight; i++ {
		prevs[i] = s.head
	}

	// Find all predecessors
	x := s.head
	level := int(atomic.LoadInt32(&s.height)) - 1

	for level >= 0 {
		next := x.next[level].Load()
		for next != nil && nodeKeyLess(next.key, key) {
			x = next
			next = x.next[level].Load()
		}

		// Check for existing key with same timestamp
		if level == 0 && next != nil && bytes.Equal(next.key.Key, key.Key) && next.key.Timestamp == key.Timestamp {
			next.value.Store(&value)
			return
		}

		prevs[level] = x
		level--
	}

	// Update height BEFORE inserting the node so that the new node
	// is visible on all levels, including newly added ones.
	for height > int(atomic.LoadInt32(&s.height)) {
		atomic.StoreInt32(&s.height, int32(height))
	}

	// Create and link new node
	node := newNode(key, value, height)
	for l := 0; l < height; l++ {
		// prevs[l] is guaranteed non-nil (initialized with head)
		node.next[l].Store(prevs[l].next[l].Load())
		prevs[l].next[l].Store(node)
	}

	atomic.AddInt64(&s.length, 1)
}

// Delete marks a key as deleted (tombstone).
// Mutex-protected write.
func (s *SkipList) Delete(key mvcc.MVCCKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	node := s.findGreaterOrEqualNoEpoch(key)
	if node == nil || !bytes.Equal(node.key.Key, key.Key) {
		return false
	}

	if node.deleted.Load() {
		return false
	}

	node.deleted.Store(true)
	atomic.AddInt64(&s.length, -1)
	return true
}

// findGreaterOrEqualNoEpoch is like findGreaterOrEqual but without epoch management.
// Used internally when mutex is held.
func (s *SkipList) findGreaterOrEqualNoEpoch(key mvcc.MVCCKey) *Node {
	x := s.head
	level := atomic.LoadInt32(&s.height) - 1

	for level >= 0 {
		next := x.next[level].Load()
		if next != nil && nodeKeyLess(next.key, key) {
			x = next
			continue
		}
		if level == 0 {
			return next
		}
		level--
	}
	return nil
}

// Len returns the number of active (non-deleted) entries.
func (s *SkipList) Len() int {
	return int(atomic.LoadInt64(&s.length))
}

// Height returns the current maximum height of the skip list.
func (s *SkipList) Height() int {
	return int(atomic.LoadInt32(&s.height))
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
		// Continue searching for the next live node
	}
}

// Key returns the current node's key.
func (it *SkipListIterator) Key() mvcc.MVCCKey {
	return it.current.key
}

// Value returns the current node's value.
func (it *SkipListIterator) Value() []byte {
	val := it.current.value.Load()
	if val == nil {
		return nil
	}
	return *val
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
