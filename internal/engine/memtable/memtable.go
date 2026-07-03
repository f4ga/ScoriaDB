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
//  1. Find the first node with key >= the search key using findGreaterOrEqual.
//     Use Timestamp = math.MaxUint64 (commitTS=0) to find the first version
//     of this user key, since inverted timestamps mean smaller commitTS
//     sorts first in the skip list ordering.
//  2. If node is nil or user key doesn't match → key doesn't exist.
//  3. Walk the entire level-0 chain for this user key:
//     - The chain is sorted by commitTS ascending (oldest first) because
//     Timestamp is inverted (MaxUint64 - commitTS) and nodeKeyLess returns
//     a.Timestamp > b.Timestamp for same key, meaning smaller commitTS
//     (larger Timestamp) sorts first.
//     - Track the newest visible version (largest commitTS <= snapshotTS).
//     - Since Timestamp is inverted, commitTS <= snapshotTS is equivalent to
//     node.key.Timestamp >= key.Timestamp.
//  4. After walking the chain:
//     - If no visible version found → key doesn't exist.
//     - If the newest visible version is a tombstone → key is deleted.
//     - Otherwise, return the value.
func (mt *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	if mt.sl == nil {
		return nil, false
	}

	// Use Timestamp = math.MaxUint64 (commitTS=0) to find the first node
	// for this user key. In the skip list ordering (nodeKeyLess), for the
	// same user key, a node with smaller commitTS sorts first because
	// Timestamp is inverted (MaxUint64 - commitTS) and nodeKeyLess returns
	// a.Timestamp > b.Timestamp. So commitTS=0 (Timestamp=MaxUint64) is
	// the "smallest" key for this user key, and findGreaterOrEqual will
	// return the first node in the chain.
	searchKey := mvcc.MVCCKey{
		Key:       key.Key,
		Timestamp: mvcc.InvertTimestamp(0), // math.MaxUint64
	}
	node := mt.sl.findGreaterOrEqual(searchKey)
	if node == nil {
		return nil, false
	}

	// Check if the user key matches.
	// If not, the key doesn't exist in this MemTable.
	nodeKey := node.Key()
	if !bytes.Equal(nodeKey.Key, key.Key) {
		return nil, false
	}

	// Walk the entire level-0 chain for this user key.
	// The chain is sorted by commitTS ascending (oldest first).
	// We need the newest version with commitTS <= snapshotTS,
	// so we track the best match as we walk.
	var bestNode *Node
	for node != nil {
		nodeKey = node.Key()
		if !bytes.Equal(nodeKey.Key, key.Key) {
			break
		}
		// Timestamp is inverted: nodeKey.Timestamp = MaxUint64 - commitTS.
		// We need commitTS <= snapshotTS, which is equivalent to:
		//   MaxUint64 - nodeKey.Timestamp <= MaxUint64 - key.Timestamp
		//   → nodeKey.Timestamp >= key.Timestamp
		if nodeKey.Timestamp >= key.Timestamp {
			// This version is visible (commitTS <= snapshotTS).
			// Since the chain is ascending by commitTS, each subsequent
			// match has a larger commitTS (newer version), so we
			// overwrite bestNode to keep the newest visible version.
			bestNode = node
		}
		node = node.next[0].Load()
	}

	if bestNode == nil {
		// No visible version found.
		return nil, false
	}

	if bestNode.deleted.Load() {
		// Tombstone → key is deleted.
		return nil, false
	}

	return bestNode.Value(), true
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
			}
		}
		node = node.next[0].Load()
	}
	return bestValue, bestTS, bestValue != nil
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
