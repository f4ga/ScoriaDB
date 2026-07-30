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

// Package memtable provides an in-memory table implementation using a lock-free
// skip list with a linear arena allocator. It supports concurrent reads and writes
// with zero heap allocations in the hot path.
package memtable

import (
	"bytes"
	"sync/atomic"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// MemTable is a lock-free in-memory skip list structure.
type MemTable struct {
	sl   *SkipList
	size int64 // atomic
}

// NewMemTable creates a new MemTable backed by a lock-free skip list.
func NewMemTable() *MemTable {
	return &MemTable{
		sl: NewSkipList(),
	}
}

// Put adds a key-value pair. In LSM, Put always creates a new version.
// Size is incremented only when a genuinely new entry is created
// (not when an existing entry with the same key+timestamp is updated).
func (mt *MemTable) Put(key mvcc.MVCCKey, value []byte) {
	if mt.sl.Put(key, value) {
		atomic.AddInt64(&mt.size, 1)
	}
}

// DeleteWithTS marks a key as deleted (tombstone).
// Tombstone is a full entry in MemTable and occupies space until flush.
func (mt *MemTable) DeleteWithTS(key mvcc.MVCCKey) {
	mt.sl.Put(key, nil)
	mt.sl.Delete(key)
	// Size is not decremented: tombstone is a real entry that will be
	// flushed to SSTable as a deleted record.
}

// Get returns the newest visible version for the given snapshotTS.
// Returns (value, true) if found and not deleted, otherwise (nil, false).
//
// Algorithm:
//  1. Find the first node with key >= searchKey using findGreaterOrEqual.
//     searchKey has Timestamp = InvertTimestamp(0) (MaxUint64).
//     For the same user key, nodes are ordered by Timestamp descending
//     (newer commitTS first).
//  2. If node is nil or user key doesn't match → key doesn't exist.
//  3. Iterate through all versions of this user key via next[0] pointers.
//  4. For each version:
//     - If timestamp is not visible (commitTS > snapshotTS) → skip
//     - If version is a tombstone (deleted) → reset bestNode to nil
//     - Otherwise → update bestNode (this is the newest visible non-deleted version)
//  5. If bestNode is nil → key is deleted or doesn't exist.
//  6. Otherwise, return the value.
func (mt *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	if mt.sl == nil {
		return nil, false
	}

	// Use findGreaterOrEqual with InvertTimestamp(0) = MaxUint64 to find
	// the newest version of this key.
	searchKey := mvcc.MVCCKey{
		Key:       key.Key,
		Timestamp: mvcc.InvertTimestamp(0),
	}
	node := mt.sl.findGreaterOrEqual(searchKey)
	if node == nil {
		return nil, false
	}

	nodeKey := node.Key()
	if nodeKey.Key == nil || !bytes.Equal(nodeKey.Key, key.Key) {
		return nil, false
	}

	// Iterate all versions of this key via next[0] chain.
	// Track the newest non-deleted version with commitTS <= snapshotTS.
	var bestNode *Node
	for node != nil {
		nodeKey = node.Key()
		if !bytes.Equal(nodeKey.Key, key.Key) {
			break
		}
		// nodeKey.Timestamp >= key.Timestamp means commitTS <= snapshotTS
		if nodeKey.Timestamp >= key.Timestamp {
			if node.deleted.Load() {
				// Tombstone: hide all older versions
				bestNode = nil
			} else {
				bestNode = node
			}
		}
		node = node.next[0].Load()
	}

	if bestNode == nil {
		return nil, false
	}

	return bestNode.Value(), true
}

// GetLatest returns the latest (newest) non-deleted value and its commit timestamp
// for a user key. Uses binary search (findGreaterOrEqual) — O(log N) instead of O(N) iterator.
// Returns (value, commitTS, found).
//
// MVCC semantics: a tombstone deletes a specific version, not the entire key.
// Older non-deleted versions remain visible. GetLatest iterates all versions
// of the key and returns the newest non-deleted one.
//
// If no non-deleted version exists (key not found or all versions are tombstones),
// returns (nil, 0, false).
//
// Algorithm:
//  1. Find the first node with key >= searchKey using findGreaterOrEqual.
//     searchKey has Timestamp = InvertTimestamp(0), which is MaxUint64.
//     For the same user key, nodes are ordered by Timestamp descending
//     (newer commitTS first, since Timestamp = MaxUint64 - commitTS).
//     findGreaterOrEqual returns the first node with key >= searchKey,
//     which for the same user key is the newest version (highest commitTS).
//  2. Iterate through all versions of this user key via next[0] pointers.
//  3. Track the newest non-deleted value (bestValue, bestTS).
//  4. Return the newest non-deleted value, or (nil, 0, false) if none found.
func (mt *MemTable) GetLatest(key []byte) ([]byte, uint64, bool) {
	if mt.sl == nil {
		return nil, 0, false
	}
	// Use findGreaterOrEqual directly — O(log N) instead of O(N) iterator
	searchKey := mvcc.MVCCKey{Key: key, Timestamp: mvcc.InvertTimestamp(0)}
	node := mt.sl.findGreaterOrEqual(searchKey)
	if node == nil {
		return nil, 0, false
	}
	nodeKey := node.Key()
	if !bytes.Equal(nodeKey.Key, key) {
		return nil, 0, false
	}

	// Iterate all versions of this key via next[0] chain.
	// Track the newest non-deleted version.
	var bestValue []byte
	var bestTS uint64
	found := false
	iterCount := 0
	for node != nil {
		nodeKey = node.Key()
		if !bytes.Equal(nodeKey.Key, key) {
			break
		}
		commitTS := nodeKey.CommitTS()
		deleted := node.deleted.Load()
		val := node.Value()
		if !deleted && commitTS > bestTS {
			bestTS = commitTS
			bestValue = val
			found = true
		}
		node = node.next[0].Load()
		iterCount++
	}

	return bestValue, bestTS, found
}

// NewIterator returns an iterator over all entries.
func (mt *MemTable) NewIterator() *MemTableIterator {
	return &MemTableIterator{
		iter: mt.sl.NewIterator(),
	}
}

// Size returns the number of entries.
func (mt *MemTable) Size() int {
	return int(atomic.LoadInt64(&mt.size))
}

// LastKey returns the last (maximum) key in the MemTable.
// Used for range scans and iteration bounds.
func (mt *MemTable) LastKey() mvcc.MVCCKey {
	if mt.sl == nil {
		return mvcc.MVCCKey{}
	}
	node := mt.sl.findLast()
	if node == nil || node == mt.sl.head {
		return mvcc.MVCCKey{}
	}
	return node.Key()
}

// Close releases resources held by MemTable.
func (mt *MemTable) Close() {
	mt.sl = nil
	atomic.StoreInt64(&mt.size, 0)
}

// MemTableIterator iterates over MemTable entries.
type MemTableIterator struct {
	iter *SkipListIterator
}

// Next advances the iterator.
func (it *MemTableIterator) Next() bool {
	return it.iter.Next()
}

// Key returns the current key.
func (it *MemTableIterator) Key() mvcc.MVCCKey {
	return it.iter.Key()
}

// Value returns the current value.
func (it *MemTableIterator) Value() []byte {
	return it.iter.Value()
}

// IsDeleted returns true if the current entry is a tombstone.
func (it *MemTableIterator) IsDeleted() bool {
	return it.iter.IsDeleted()
}

// Close releases iterator resources.
func (it *MemTableIterator) Close() {
	it.iter.Close()
}
