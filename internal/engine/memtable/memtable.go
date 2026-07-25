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
//  1. Find the last node with key <= searchKey using findLessOrEqual.
//     The searchKey has Timestamp = InvertTimestamp(snapshotTS).
//     For the same user key, nodes are ordered by Timestamp descending
//     (newer commitTS first, since Timestamp = MaxUint64 - commitTS).
//     findLessOrEqual returns the node with the largest key <= searchKey,
//     which for the same user key is the newest version with
//     commitTS <= snapshotTS.
//  2. If node is nil or user key doesn't match → key doesn't exist.
//  3. If node is deleted (tombstone) → key is deleted.
//  4. Otherwise, return the value.
//
// See: ARCH-07, MVCC-03
func (mt *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	if mt.sl == nil {
		return nil, false
	}

	// findLessOrEqual returns the last node with key <= searchKey.
	// For the same user key, nodes are ordered by Timestamp descending
	// (newer commitTS first). findLessOrEqual finds the newest version
	// with commitTS <= snapshotTS because:
	//   - Timestamp = MaxUint64 - commitTS
	//   - nodeKeyLess: for same key, a.Timestamp > b.Timestamp means a < b
	//   - So larger commitTS = smaller Timestamp = "less" in ordering
	//   - findLessOrEqual finds the last node <= searchKey
	//   - This is the newest version with Timestamp >= key.Timestamp
	//   - Which means commitTS <= snapshotTS
	node := mt.sl.findLessOrEqual(key)
	if node == nil {
		return nil, false
	}

	// Check if the user key matches.
	nodeKey := node.Key()
	if nodeKey.Key == nil || !bytes.Equal(nodeKey.Key, key.Key) {
		return nil, false
	}

	if node.deleted.Load() {
		// Tombstone → key is deleted.
		return nil, false
	}

	return node.Value(), true
}

// GetLatest returns the latest (newest) value and its commit timestamp for a user key.
// Uses binary search (findGreaterOrEqual) — O(log N) instead of O(N) iterator.
// Returns (value, commitTS, found). The value is the raw stored value (may be a ValuePointer).
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

	var bestValue []byte
	var bestTS uint64
	found := false
	for node != nil {
		nodeKey = node.Key()
		if !bytes.Equal(nodeKey.Key, key) {
			break
		}
		if !node.deleted.Load() {
			commitTS := nodeKey.CommitTS()
			if commitTS > bestTS {
				bestTS = commitTS
				bestValue = node.Value()
				found = true
			}
		}
		node = node.next[0].Load()
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
