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

// Package memtable provides an in-memory table implementation for LSM.
package memtable

import (
	"bytes"
	"sync/atomic"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// MemTable wraps a SkipList and provides methods for LSM operations.
type MemTable struct {
	sl *SkipList
}

// NewMemTable creates a new MemTable with its OWN fresh flat arena.
//
// Each MemTable must own its own arena: during flush the active and frozen
// tables must never share a FlatArena, because writes to the active table would
// land in the same block and could overwrite nodes the frozen table still
// references while it is iterated. See: ARENA-01.
func NewMemTable() *MemTable {
	return &MemTable{sl: NewSkipList()}
}

// NewMemTableWithArena creates a new MemTable backed by the given arena.
// The caller is responsible for ensuring the arena is not shared between an
// active and a concurrently-flushed frozen MemTable. See: ARENA-01.
func NewMemTableWithArena(arena *Arena) *MemTable {
	sl := NewSkipList()
	sl.arena = arena
	return &MemTable{sl: sl}
}

// Put inserts a key-value pair.
func (m *MemTable) Put(key mvcc.MVCCKey, value []byte) bool {
	return m.sl.Put(key, value)
}

// Get retrieves a value for the given key.
func (m *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	return m.sl.Get(key)
}

// Delete marks a key as deleted (tombstone).
func (m *MemTable) Delete(key mvcc.MVCCKey) bool {
	return m.sl.Delete(key)
}

// DeleteWithTS marks a key as deleted (tombstone) for the given MVCC version.
// It creates a tombstone at the given timestamp even if no exact version exists.
// It is used during WAL recovery to correctly place tombstones.
func (m *MemTable) DeleteWithTS(key mvcc.MVCCKey) bool {
	return m.sl.DeleteWithTS(key)
}

// Len returns the number of active entries.
func (m *MemTable) Len() int {
	return m.sl.Len()
}

// Size returns the number of active entries.
// It is an alias for Len, used by flush to size SSTable writers.
func (m *MemTable) Size() int {
	return m.sl.Len()
}

// SizeBytes returns the total number of bytes actually allocated in the arena
// backing this memtable. This reflects real memory usage (not just logical
// bytes of key+value), so the flush worker can trigger on actual arena
// occupancy rather than waiting for it to silently fill hundreds of MB.
// See: PERF-01, SYMPTOM-03
func (m *MemTable) SizeBytes() uint64 {
	if m.sl == nil || m.sl.Arena() == nil {
		return 0
	}
	return m.sl.Arena().Size()
}

// LastKey returns the key of the last active (non‑deleted) entry in the table.
// Returns a nil key if the table is empty.
func (m *MemTable) LastKey() mvcc.MVCCKey {
	idx := m.sl.FindLast()
	if idx == 0 {
		return mvcc.MVCCKey{Key: nil}
	}
	return m.sl.NodeAt(idx).Key()
}

// Close releases the memtable resources.
// Resets the underlying skip list, invalidating all previously stored data.
func (m *MemTable) Close() {
	m.sl.Reset()
}

// NewIterator returns an iterator over the table.
func (m *MemTable) NewIterator() *SkipListIterator {
	return m.sl.NewIterator()
}

// GetLatest returns the latest live value and its commit timestamp for a key (MVCC).
// It walks the version chain of the key (from oldest to newest) and returns the
// newest non‑deleted version. If all versions are tombstones, it returns
// (nil, tombstoneTS, false). If the key is entirely absent, it returns (nil, 0, false).
func (m *MemTable) GetLatest(key []byte) ([]byte, uint64, bool) {
	// Hold an EBR epoch for the ENTIRE read so the arena is not reset while we
	// traverse the version chain and dereference nodes.
	m.sl.epoch.EnterEpoch()
	defer m.sl.epoch.ExitEpoch()

	// The oldest version of the key has the maximum (inverted) timestamp,
	// so findGreaterOrEqual locates the first node for this key.
	start := mvcc.MVCCKey{Key: key, Timestamp: ^uint64(0)}
	idx := m.sl.findGreaterOrEqualNoEpoch(start)
	if idx == 0 {
		return nil, 0, false
	}

	var latestVal []byte
	var latestTS uint64
	found := false
	tombstoneTS := uint64(0)
	hasTombstone := false

	for idx != 0 {
		node := m.sl.nodeAt(idx)
		nk := node.Key()
		if !bytes.Equal(nk.Key, key) {
			break
		}
		ts := nk.CommitTS()
		// Atomic read: writers store `deleted` via atomic.StoreUint32 while
		// readers run concurrently.
		if atomic.LoadUint32(&node.deleted) == 0 {
			latestVal = node.Value()
			latestTS = ts
			found = true
		} else {
			hasTombstone = true
			tombstoneTS = ts
		}
		// Atomic read of next[0] for the same reason.
		idx = atomic.LoadUint32(&node.next[0])
	}

	if found {
		return latestVal, latestTS, true
	}
	if hasTombstone {
		return nil, tombstoneTS, false
	}
	return nil, 0, false
}

// GetWithTS retrieves the value for the given key and timestamp (MVCC).
func (m *MemTable) GetWithTS(key mvcc.MVCCKey) ([]byte, bool) {
	return m.sl.Get(key)
}
