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
	"bytes"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// SkipList is a concurrent skip list with lock‑free reads and mutex‑protected writes.
// It uses a flat arena with indices instead of pointers for better cache locality.
// MVCC is fully supported.
type SkipList struct {
	arena      *Arena
	headIdx    uint32
	height     int32
	length     int64
	mu         sync.Mutex
	epoch      *EpochManager
	lastActive uint32
}

// NewSkipList creates a new empty skip list with a flat arena.
func NewSkipList() *SkipList {
	arena := NewArena()
	head := arena.NewNode(nil, nil, MaxHeight)
	headIdx := arena.Index(head)
	return &SkipList{
		arena:      arena,
		headIdx:    headIdx,
		height:     1,
		epoch:      NewEpochManager(1000),
		lastActive: 0,
	}
}

// nodeAt returns a pointer to the node by its index.
func (s *SkipList) nodeAt(idx uint32) *Node {
	return s.arena.NodeAt(idx)
}

// NodeAt returns a pointer to the node by its index.
// Exported for external consumers (e.g. LastKey).
func (s *SkipList) NodeAt(idx uint32) *Node {
	return s.arena.NodeAt(idx)
}

// nodeKeyLess compares two MVCC keys in skip list order.
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

// findGreaterOrEqual returns the index of the first node with key >= the given key.
// findGreaterOrEqualNoEpoch returns the index of the first node with key >= the
// given key. Lock‑free traversal. The caller MUST hold an EBR epoch (see
// findGreaterOrEqual / Get / GetLatest) so the arena is not reset concurrently.
func (s *SkipList) findGreaterOrEqualNoEpoch(key mvcc.MVCCKey) uint32 {
	idx := s.headIdx
	level := atomic.LoadInt32(&s.height) - 1

	for level >= 0 {
		nextIdx := atomic.LoadUint32(&s.nodeAt(idx).next[level])
		for nextIdx != 0 && nodeKeyLess(s.nodeAt(nextIdx).Key(), key) {
			idx = nextIdx
			nextIdx = atomic.LoadUint32(&s.nodeAt(idx).next[level])
		}
		if level == 0 {
			return nextIdx
		}
		level--
	}
	return 0
}

// Lock‑free traversal, protected by EBR. See Глава IV.3.
func (s *SkipList) findGreaterOrEqual(key mvcc.MVCCKey) uint32 {
	s.epoch.EnterEpoch()
	defer s.epoch.ExitEpoch()
	return s.findGreaterOrEqualNoEpoch(key)
}

// findExact returns the index of the node with exact key+timestamp match.
func (s *SkipList) findExact(key mvcc.MVCCKey) uint32 {
	nodeIdx := s.findGreaterOrEqual(key)
	if nodeIdx == 0 {
		return 0
	}
	node := s.nodeAt(nodeIdx)
	nk := node.Key()
	if nk.Key == nil || !bytes.Equal(nk.Key, key.Key) || nk.Timestamp != key.Timestamp {
		return 0
	}
	return nodeIdx
}

// Get retrieves the value for the given key at the snapshot timestamp.
// It walks the version chain of the key (ordered oldest → newest) and returns
// the last live version with CommitTS <= snapshot. A tombstone at or before the
// snapshot hides older versions. See: ARCH-07 (MVCC inverted timestamp).
func (s *SkipList) Get(key mvcc.MVCCKey) ([]byte, bool) {
	// Hold an EBR epoch for the ENTIRE read so the arena is not reset while we
	// walk the version chain and dereference nodes. See DEF-02, Глава IV.3.
	s.epoch.EnterEpoch()
	defer s.epoch.ExitEpoch()

	// The oldest version of the key has the maximum inverted timestamp, so
	// findGreaterOrEqual with MaxUint64 locates the first node for this key.
	oldest := s.findGreaterOrEqualNoEpoch(mvcc.MVCCKey{Key: key.Key, Timestamp: ^uint64(0)})
	if oldest == 0 {
		return nil, false
	}
	first := s.nodeAt(oldest)
	if first.Key().Key == nil || !bytes.Equal(first.Key().Key, key.Key) {
		return nil, false
	}

	snapshotTS := key.CommitTS()
	idx := oldest
	var result []byte
	found := false
	for idx != 0 {
		n := s.nodeAt(idx)
		nk := n.Key()
		if !bytes.Equal(nk.Key, key.Key) {
			break
		}
		if nk.CommitTS() > snapshotTS {
			break
		}
		if atomic.LoadUint32(&n.deleted) == 0 {
			result = n.Value()
			found = true
		} else {
			// Tombstone at/before snapshot hides older versions.
			result = nil
			found = false
		}
		idx = atomic.LoadUint32(&n.next[0])
	}
	return result, found
}

// Put inserts or updates a key‑value pair.
func (s *SkipList) Put(key mvcc.MVCCKey, value []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	height := randomHeight()

	var prevs [MaxHeight]uint32
	for i := 0; i < MaxHeight; i++ {
		prevs[i] = s.headIdx
	}

	idx := s.headIdx
	level := int(atomic.LoadInt32(&s.height)) - 1
	var existingNodeIdx uint32

	for level >= 0 {
		nextIdx := atomic.LoadUint32(&s.nodeAt(idx).next[level])
		for nextIdx != 0 && nodeKeyLess(s.nodeAt(nextIdx).Key(), key) {
			idx = nextIdx
			nextIdx = atomic.LoadUint32(&s.nodeAt(idx).next[level])
		}
		if level == 0 && nextIdx != 0 {
			n := s.nodeAt(nextIdx)
			nk := n.Key()
			if bytes.Equal(nk.Key, key.Key) && nk.Timestamp == key.Timestamp {
				existingNodeIdx = nextIdx
			}
		}
		prevs[level] = idx
		level--
	}

	if existingNodeIdx != 0 {
		return false
	}

	for height > int(atomic.LoadInt32(&s.height)) {
		atomic.StoreInt32(&s.height, int32(height))
	}

	node := s.arena.NewNode(key.Key, value, height)
	node.ts = key.Timestamp

	nodeIdx := s.arena.Index(node)

	for l := 0; l < height; l++ {
		nextIdx := atomic.LoadUint32(&s.nodeAt(prevs[l]).next[l])
		atomic.StoreUint32(&node.next[l], nextIdx)
		atomic.StoreUint32(&s.nodeAt(prevs[l]).next[l], nodeIdx)
	}

	s.updateLastActive(nodeIdx)
	atomic.AddInt64(&s.length, 1)
	return true
}

// Delete marks an existing exact version as deleted (tombstone).
// It only affects a node that already exists with the exact key+timestamp.
func (s *SkipList) Delete(key mvcc.MVCCKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodeIdx := s.findExact(key)
	if nodeIdx == 0 {
		return false
	}
	node := s.nodeAt(nodeIdx)
	if atomic.LoadUint32(&node.deleted) == 1 {
		return false
	}
	atomic.StoreUint32(&node.deleted, 1)

	if s.lastActive == nodeIdx {
		s.lastActive = 0
	}

	s.epoch.Retire(unsafe.Pointer(node))
	atomic.AddInt64(&s.length, -1)
	return true
}

// DeleteWithTS marks the given MVCC version as a tombstone, creating a tombstone
// node if no exact version exists. It is used for MVCC deletes at an arbitrary
// commitTS (e.g. WAL recovery) where a delete must hide the key at snapshots
// >= the tombstone timestamp. See: PROMPT-TOMBSTONE-BATCH-FIX.
func (s *SkipList) DeleteWithTS(key mvcc.MVCCKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodeIdx := s.findExact(key)
	if nodeIdx != 0 {
		node := s.nodeAt(nodeIdx)
		if atomic.LoadUint32(&node.deleted) == 1 {
			return false
		}
		atomic.StoreUint32(&node.deleted, 1)
		if s.lastActive == nodeIdx {
			s.lastActive = 0
		}
		s.epoch.Retire(unsafe.Pointer(node))
		atomic.AddInt64(&s.length, -1)
		return true
	}

	// No exact version exists — insert a tombstone node at this timestamp.
	// The tombstone is not an active entry, so length is unchanged.
	height := randomHeight()
	var prevs [MaxHeight]uint32
	for i := 0; i < MaxHeight; i++ {
		prevs[i] = s.headIdx
	}
	idx := s.headIdx
	level := int(atomic.LoadInt32(&s.height)) - 1
	for level >= 0 {
		nextIdx := atomic.LoadUint32(&s.nodeAt(idx).next[level])
		for nextIdx != 0 && nodeKeyLess(s.nodeAt(nextIdx).Key(), key) {
			idx = nextIdx
			nextIdx = atomic.LoadUint32(&s.nodeAt(idx).next[level])
		}
		prevs[level] = idx
		level--
	}
	for height > int(atomic.LoadInt32(&s.height)) {
		atomic.StoreInt32(&s.height, int32(height))
	}

	node := s.arena.NewNode(key.Key, nil, height)
	node.ts = key.Timestamp
	atomic.StoreUint32(&node.deleted, 1)

	nodeIdx = s.arena.Index(node)
	for l := 0; l < height; l++ {
		nextIdx := atomic.LoadUint32(&s.nodeAt(prevs[l]).next[l])
		atomic.StoreUint32(&node.next[l], nextIdx)
		atomic.StoreUint32(&s.nodeAt(prevs[l]).next[l], nodeIdx)
	}
	s.updateLastActive(nodeIdx)
	return true
}

// Len returns the number of active (non‑deleted) entries.
func (s *SkipList) Len() int {
	return int(atomic.LoadInt64(&s.length))
}

// Height returns the current max height.
func (s *SkipList) Height() int {
	return int(atomic.LoadInt32(&s.height))
}

// Arena returns the arena backing this skip list.
func (s *SkipList) Arena() *Arena {
	return s.arena
}

// FindLast returns the index of the last active (non‑deleted) node.
// Returns 0 if the list is empty.
func (s *SkipList) FindLast() uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastActive != 0 {
		return s.lastActive
	}

	// Traverse level 0 from head.
	idx := s.headIdx
	var lastIdx uint32
	for {
		nextIdx := atomic.LoadUint32(&s.nodeAt(idx).next[0])
		if nextIdx == 0 {
			break
		}
		if atomic.LoadUint32(&s.nodeAt(nextIdx).deleted) == 0 {
			lastIdx = nextIdx
		}
		idx = nextIdx
	}
	if lastIdx != 0 {
		s.lastActive = lastIdx
	}
	return lastIdx
}

// updateLastActive updates the cached lastActive index.
func (s *SkipList) updateLastActive(nodeIdx uint32) {
	node := s.nodeAt(nodeIdx)
	if atomic.LoadUint32(&node.deleted) == 1 {
		return
	}
	if s.lastActive == 0 {
		s.lastActive = nodeIdx
		return
	}
	lastNode := s.nodeAt(s.lastActive)
	if nodeKeyLess(lastNode.Key(), node.Key()) {
		s.lastActive = nodeIdx
	}
}

// Reset clears the skip list, reinitializing the arena and head node.
// All previously allocated nodes become invalid. Cold path (used by Close).
//
// EBR SAFETY (DEF-02, Глава IV.3, Глава IX):
// Before zeroing the arena we must guarantee that no active reader still holds
// a reference to any node. We quiesce the epoch manager (close the reader gate
// and wait for activeReaders==0), then reset the arena, then reopen the gate.
// This prevents use-after-free when readers call Get/findGreaterOrEqual
// concurrently with Close/Reset.
func (s *SkipList) Reset() {
	// Phase 1: close the reader gate and wait for all in-flight readers to exit.
	// No new reader can enter after Quiesce returns.
	s.epoch.Quiesce()

	// Phase 2: perform the reset. Readers cannot enter while the gate is closed,
	// so we hold s.mu (to exclude writers) and mutate the arena safely.
	s.mu.Lock()

	// Reclaim retired nodes (drain the EBR retired list) — safe now.
	s.epoch.Clean()

	s.arena.Reset()
	head := s.arena.NewNode(nil, nil, MaxHeight)
	s.headIdx = s.arena.Index(head)
	atomic.StoreInt32(&s.height, 1)
	atomic.StoreInt64(&s.length, 0)
	s.lastActive = 0

	s.mu.Unlock()

	// Phase 3: reopen the reader gate.
	s.epoch.ResumeQuiescence()
}

// ------------------------------------------------------------
// Iterator
// ------------------------------------------------------------

// SkipListIterator provides forward iteration over the skip list.
type SkipListIterator struct {
	current uint32
	list    *SkipList
	done    bool
}

// NewIterator creates a new iterator positioned before the first element.
func (s *SkipList) NewIterator() *SkipListIterator {
	return &SkipListIterator{
		current: s.headIdx,
		list:    s,
	}
}

// Next advances the iterator to the next non‑deleted node.
func (it *SkipListIterator) Next() bool {
	if it.done {
		return false
	}
	for {
		nextIdx := atomic.LoadUint32(&it.list.nodeAt(it.current).next[0])
		if nextIdx == 0 {
			it.done = true
			return false
		}
		it.current = nextIdx
		if atomic.LoadUint32(&it.list.nodeAt(it.current).deleted) == 0 {
			return true
		}
	}
}

// Key returns the current node's key.
func (it *SkipListIterator) Key() mvcc.MVCCKey {
	return it.list.nodeAt(it.current).Key()
}

// Value returns the current node's value.
func (it *SkipListIterator) Value() []byte {
	return it.list.nodeAt(it.current).Value()
}

// IsDeleted returns true if the current node is a tombstone.
func (it *SkipListIterator) IsDeleted() bool {
	if it.current == 0 {
		return false
	}
	return atomic.LoadUint32(&it.list.nodeAt(it.current).deleted) == 1
}

// Close releases iterator resources.
func (it *SkipListIterator) Close() {
	it.current = 0
	it.list = nil
	it.done = true
}

// ------------------------------------------------------------
// Node methods (compatibility)
// ------------------------------------------------------------

// Key returns the MVCCKey of the node.
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
func (n *Node) Value() []byte {
	if atomic.LoadUint32(&n.deleted) == 1 {
		return nil
	}
	if n.valOff == 0 {
		return nil
	}
	if n.valLen == 0 {
		return []byte{}
	}
	ptr := unsafe.Add(unsafe.Pointer(n), n.valOff)
	return unsafe.Slice((*byte)(ptr), int(n.valLen))
}

// IsDeleted returns true if the node is a tombstone.
func (n *Node) IsDeleted() bool {
	return atomic.LoadUint32(&n.deleted) == 1
}
