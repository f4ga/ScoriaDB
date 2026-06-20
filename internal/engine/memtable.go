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

package engine

import (
	"bytes"
	"sync"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
	"github.com/google/btree"
)

// mvccEntry represents an entry in MemTable.
type mvccEntry struct {
	Key     mvcc.MVCCKey
	Value   []byte
	Deleted bool
}

// Less implements btree.Item.
func (e mvccEntry) Less(than btree.Item) bool {
	other, ok := than.(mvccEntry)
	if !ok {
		return false
	}
	return e.Key.Less(other.Key)
}

// MemTable is a thread-safe in-memory B-tree structure.
type MemTable struct {
	mu   sync.RWMutex
	tree *btree.BTree
	size int
}

// NewMemTable creates a new MemTable.
func NewMemTable() *MemTable {
	return &MemTable{
		tree: btree.New(32),
	}
}

// Put adds or updates a key-value pair.
func (mt *MemTable) Put(key mvcc.MVCCKey, value []byte) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	entry := mvccEntry{Key: key, Value: value, Deleted: false}
	if mt.tree.Has(entry) {
		mt.tree.Delete(entry)
	} else {
		mt.size++
	}
	mt.tree.ReplaceOrInsert(entry)
}

// DeleteWithTS marks a key as deleted (tombstone).
func (mt *MemTable) DeleteWithTS(key mvcc.MVCCKey) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	entry := mvccEntry{Key: key, Value: nil, Deleted: true}
	if mt.tree.Has(entry) {
		mt.tree.Delete(entry)
	} else {
		mt.size++
	}
	mt.tree.ReplaceOrInsert(entry)
}

// Get returns the newest visible version for the given snapshotTS.
// Returns (value, true) if found and not deleted, otherwise (nil, false).
func (mt *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	var result []byte
	found := false

	mt.tree.DescendLessOrEqual(mvccEntry{Key: key}, func(item btree.Item) bool {
		entry, ok := item.(mvccEntry)
		if !ok {
			return true
		}

		if !bytes.Equal(entry.Key.Key, key.Key) {
			return false
		}

		if entry.Key.Timestamp >= key.Timestamp {
			if entry.Deleted {
				return false
			}
			result = entry.Value
			found = true
			return false
		}
		return true
	})

	return result, found
}

// NewIterator returns an iterator over all entries.
func (mt *MemTable) NewIterator() *MemTableIterator {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	var entries []mvccEntry
	mt.tree.Ascend(func(item btree.Item) bool {
		entry, ok := item.(mvccEntry)
		if ok {
			entries = append(entries, entry)
		}
		return true
	})
	return &MemTableIterator{
		entries: entries,
		index:   -1,
	}
}

// Size returns the number of entries.
func (mt *MemTable) Size() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.size
}

// MemTableIterator iterates over MemTable entries.
type MemTableIterator struct {
	entries []mvccEntry
	index   int
}

// Next advances the iterator.
func (it *MemTableIterator) Next() bool {
	it.index++
	return it.index < len(it.entries)
}

// Key returns the current key.
func (it *MemTableIterator) Key() mvcc.MVCCKey {
	if it.index < 0 || it.index >= len(it.entries) {
		return mvcc.MVCCKey{}
	}
	return it.entries[it.index].Key
}

// Value returns the current value.
func (it *MemTableIterator) Value() []byte {
	if it.index < 0 || it.index >= len(it.entries) {
		return nil
	}
	return it.entries[it.index].Value
}

// Close releases iterator resources.
func (it *MemTableIterator) Close() {
	it.entries = nil
}

// Close releases resources held by MemTable.
func (mt *MemTable) Close() {
	mt.mu.Lock()
	defer mt.mu.Unlock()
	mt.tree = nil
	mt.size = 0
}
