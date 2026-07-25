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
	"container/heap"
	"fmt"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
)

// Iterator is the public iterator interface for streaming key-value pairs.
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Err() error
	Close() error

	// IsDeleted returns true if the current entry is a tombstone.
	// Optional: implementations may return false by default.
	IsDeleted() bool
}

// ============================================================
// Heap-based Merge Iterator
// ============================================================

type heapIter struct {
	iter Iterator
	key  []byte
}

type iterHeap []*heapIter

func (h iterHeap) Len() int            { return len(h) }
func (h iterHeap) Less(i, j int) bool  { return bytes.Compare(h[i].key, h[j].key) < 0 }
func (h iterHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *iterHeap) Push(x interface{}) { *h = append(*h, x.(*heapIter)) }
func (h *iterHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// mergeIterator streams sorted key-value pairs from multiple sources.
type mergeIterator struct {
	heap   iterHeap
	err    error
	key    []byte
	value  []byte
	engine *LSMEngine  // for VLog resolution in Value()
	views  []*VLogView // active VLog views, released in Close()
}

// NewMergeIterator creates a merge iterator from all available sources.
// Allocations: O(number_of_sources), not O(number_of_entries).
func NewMergeIterator(e *LSMEngine, prefix []byte) *mergeIterator {
	fmt.Printf("[DEBUG-05] NewMergeIterator: prefix=%q, memTable=%p\n", string(prefix), e.memTable)
	sources := 2
	for _, level := range e.levels {
		sources += len(level)
	}
	mi := &mergeIterator{
		heap:   make(iterHeap, 0, sources),
		engine: e,
	}

	if e.memTable != nil {
		fmt.Printf("[DEBUG-05] NewMergeIterator: adding memTable source\n")
		mi.addSource(newMemtableIter(e.memTable, prefix))
	} else {
		fmt.Printf("[DEBUG-05] NewMergeIterator: memTable is nil\n")
	}
	if e.frozenMemTable != nil {
		fmt.Printf("[DEBUG-05] NewMergeIterator: adding frozenMemTable source\n")
		mi.addSource(newMemtableIter(e.frozenMemTable, prefix))
	} else {
		fmt.Printf("[DEBUG-05] NewMergeIterator: frozenMemTable is nil\n")
	}
	for li, level := range e.levels {
		for si, sst := range level {
			fmt.Printf("[DEBUG-05] NewMergeIterator: adding SSTable source at level[%d][%d]\n", li, si)
			mi.addSource(newSSTableIter(sst, prefix))
		}
	}

	heap.Init(&mi.heap)
	fmt.Printf("[DEBUG-05] NewMergeIterator: heap initialized with %d sources\n", len(mi.heap))
	return mi
}

func (mi *mergeIterator) addSource(iter Iterator) {
	if iter == nil {
		fmt.Printf("[DEBUG-06] addSource: iter is nil, skipping\n")
		return
	}
	fmt.Printf("[DEBUG-06] addSource: checking first element\n")
	if !iter.Next() {
		fmt.Printf("[DEBUG-06] addSource: iter.Next() returned false (empty source), err=%v\n", iter.Err())
		if err := iter.Err(); err != nil {
			mi.err = err
		}
		iter.Close()
		return
	}
	key := iter.Key()
	fmt.Printf("[DEBUG-06] addSource: adding source, first key=%q\n", string(key))
	mi.heap = append(mi.heap, &heapIter{
		iter: iter,
		key:  key,
	})
}

func (mi *mergeIterator) Next() bool {
	fmt.Printf("[DEBUG-09] mergeIterator.Next() called, heap len=%d, err=%v\n", len(mi.heap), mi.err)
	if mi.err != nil || len(mi.heap) == 0 {
		if len(mi.heap) == 0 {
			fmt.Printf("[DEBUG-09] mergeIterator.Next(): heap is empty\n")
		}
		if mi.err != nil {
			fmt.Printf("[DEBUG-09] mergeIterator.Next(): error: %v\n", mi.err)
		}
		return false
	}

	hi := heap.Pop(&mi.heap).(*heapIter)
	mi.key = hi.key
	mi.value = hi.iter.Value()
	fmt.Printf("[DEBUG-09] mergeIterator.Next(): popped key=%q, value len=%d\n", string(mi.key), len(mi.value))

	if hi.iter.Next() {
		hi.key = hi.iter.Key()
		heap.Push(&mi.heap, hi)
		fmt.Printf("[DEBUG-09] mergeIterator.Next(): source has more, pushed key=%q\n", string(hi.key))
	} else {
		if err := hi.iter.Err(); err != nil {
			mi.err = err
			hi.iter.Close()
			fmt.Printf("[DEBUG-09] mergeIterator.Next(): source error: %v\n", err)
			return false
		}
		hi.iter.Close()
		fmt.Printf("[DEBUG-09] mergeIterator.Next(): source exhausted, closed\n")
	}

	// Deduplicate: skip same keys from lower-priority sources
	skipped := 0
	for len(mi.heap) > 0 && bytes.Equal(mi.heap[0].key, mi.key) {
		hi := heap.Pop(&mi.heap).(*heapIter)
		skipped++
		if hi.iter.Next() {
			hi.key = hi.iter.Key()
			heap.Push(&mi.heap, hi)
		} else {
			if err := hi.iter.Err(); err != nil {
				mi.err = err
				hi.iter.Close()
				return false
			}
			hi.iter.Close()
		}
	}
	if skipped > 0 {
		fmt.Printf("[DEBUG-09] mergeIterator.Next(): deduplicated %d entries\n", skipped)
	}

	return true
}

func (mi *mergeIterator) Key() []byte { return mi.key }

// Value returns the current value.
// Resolves ValuePointer from either Unified MMap (new) or VLog (legacy).
// See: decodeStoredValue for the same logic in Get path.
// HOT-02, ARCH-06
func (mi *mergeIterator) Value() []byte {
	if mi.value == nil {
		return nil
	}

	// Check if this is a ValuePointer (12 bytes) pointing to large value storage.
	if len(mi.value) == ValuePointerSize {
		vp, ok := DecodeValuePointer(mi.value)
		if !ok || vp.Offset < 0 || vp.Size <= 0 {
			// Invalid ValuePointer — return as-is (should not happen).
			return mi.value
		}

		// 1. Check Unified MMap first (v0.3.0+ hot path).
		if mi.engine.unifiedMmap != nil && vp.Offset < mi.engine.unifiedMmap.Size() {
			value, err := mi.engine.unifiedMmap.ReadValue(uint64(vp.Offset), vp.Size)
			if err == nil {
				return value
			}
			// If Unified MMap read fails, fall through to VLog.
		}

		// 2. Fall back to VLog (legacy path).
		if vp.Offset+int64(vp.Size)+8 <= mi.engine.vlog.Size() {
			view, err := mi.engine.vlog.ReadView(vp)
			if err != nil {
				mi.err = err
				return nil
			}
			// Store view for later Release in Close().
			mi.views = append(mi.views, view)
			return view.Data()
		}

		// 3. Both sources failed — value pointer out of range.
		mi.err = fmt.Errorf("value pointer out of range: offset=%d size=%d", vp.Offset, vp.Size)
		return nil
	}

	// Inline value (small, stored directly in MemTable).
	return mi.value
}

func (mi *mergeIterator) Err() error      { return mi.err }
func (mi *mergeIterator) IsDeleted() bool { return false }

func (mi *mergeIterator) Close() error {
	var firstErr error
	for _, hi := range mi.heap {
		if err := hi.iter.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// Release all VLog views.
	for _, view := range mi.views {
		view.Release()
	}
	mi.heap = nil
	mi.key = nil
	mi.value = nil
	mi.views = nil
	return firstErr
}

// ============================================================
// MemTable Iterator (prefix filter + skip to newest version)
// ============================================================

type memtableIter struct {
	inner  *memtable.MemTableIterator
	prefix []byte
	key    []byte
	value  []byte
	err    error
	ended  bool
	ready  bool // true if inner is already positioned at a valid entry (from skipNewest)
	isDel  bool // deleted state of the current entry
}

func newMemtableIter(mt *memtable.MemTable, prefix []byte) *memtableIter {
	fmt.Printf("[DEBUG-07] newMemtableIter: prefix=%q, mt=%p\n", string(prefix), mt)
	return &memtableIter{
		inner:  mt.NewIterator(),
		prefix: prefix,
	}
}

func (it *memtableIter) Next() bool {
	fmt.Printf("[DEBUG-08] memtableIter.Next() called, ready=%v, ended=%v\n", it.ready, it.ended)
	if it.ended || it.err != nil {
		fmt.Printf("[DEBUG-08] memtableIter.Next(): ended or error\n")
		return false
	}

	for {
		// Advance inner iterator only if not already positioned by skipNewest
		if !it.ready {
			if !it.inner.Next() {
				it.ended = true
				fmt.Printf("[DEBUG-08] memtableIter.Next(): inner exhausted\n")
				return false
			}
			fmt.Printf("[DEBUG-08] memtableIter.Next(): inner advanced\n")
		}
		it.ready = false

		mvccKey := it.inner.Key()
		userKey := mvccKey.Key
		fmt.Printf("[DEBUG-08] memtableIter.Next(): userKey=%q, prefix=%q, match=%v\n",
			string(userKey), string(it.prefix), bytes.HasPrefix(userKey, it.prefix))

		if !bytes.HasPrefix(userKey, it.prefix) {
			fmt.Printf("[DEBUG-08] memtableIter.Next(): prefix mismatch, continuing\n")
			continue
		}

		// Track deleted state from the inner iterator.
		// Note: the skip list iterator already skips deleted nodes internally,
		// so IsDeleted() will typically return false. But we capture it here
		// for correctness in case the inner iterator state changes.
		it.isDel = it.inner.IsDeleted()

		// Skip to the newest version of this user key.
		// The skip list iterator iterates from oldest to newest for the same key.
		it.key = userKey
		it.value = it.inner.Value()
		fmt.Printf("[DEBUG-08] memtableIter.Next(): found key=%q, value len=%d, isDel=%v\n",
			string(userKey), len(it.value), it.isDel)
		it.skipToNewest(userKey)
		fmt.Printf("[DEBUG-08] memtableIter.Next(): returning true\n")
		return true
	}
}

// skipToNewest advances past all remaining versions of the same user key,
// keeping the value of the newest (last) version.
// After this call, the inner iterator is positioned at the next different key
// (or at end), and it.ready is set to prevent double-advance in Next().
// Does NOT set it.ended — the next call to Next() will handle end naturally.
func (it *memtableIter) skipToNewest(userKey []byte) {
	for {
		if !it.inner.Next() {
			// Inner iterator exhausted — next Next() call will return false.
			return
		}
		if !bytes.Equal(it.inner.Key().Key, userKey) {
			it.ready = true // positioned at next key, don't advance again in Next()
			return
		}
		// Same key, newer version — update value
		it.value = it.inner.Value()
	}
}

func (it *memtableIter) Key() []byte     { return it.key }
func (it *memtableIter) Value() []byte   { return it.value }
func (it *memtableIter) Err() error      { return it.err }
func (it *memtableIter) IsDeleted() bool { return it.isDel }

func (it *memtableIter) Close() error {
	it.inner.Close()
	it.ended = true
	return nil
}

// ============================================================
// SSTable Iterator
// ============================================================

type sstableIter struct {
	inner  *sstable.SSTableIterator
	prefix []byte
	key    []byte
	value  []byte
	err    error
	ended  bool
}

func newSSTableIter(sst *sstable.Reader, prefix []byte) *sstableIter {
	inner, err := sst.NewIterator()
	if err != nil {
		return &sstableIter{err: err, ended: true}
	}
	return &sstableIter{
		inner:  inner,
		prefix: prefix,
	}
}

func (it *sstableIter) Next() bool {
	if it.ended || it.err != nil {
		return false
	}

	for it.inner.Next() {
		mvccKey := it.inner.Key()
		userKey := mvccKey.Key

		if !bytes.HasPrefix(userKey, it.prefix) {
			continue
		}

		it.key = userKey
		it.value = it.inner.Value()
		return true
	}

	it.ended = true
	return false
}

func (it *sstableIter) Key() []byte     { return it.key }
func (it *sstableIter) Value() []byte   { return it.value }
func (it *sstableIter) Err() error      { return it.err }
func (it *sstableIter) IsDeleted() bool { return false }

func (it *sstableIter) Close() error {
	it.inner.Close()
	it.ended = true
	return nil
}

// ============================================================
// Empty Iterator
// ============================================================

type emptyIterator struct{}

func (it *emptyIterator) Next() bool      { return false }
func (it *emptyIterator) Key() []byte     { return nil }
func (it *emptyIterator) Value() []byte   { return nil }
func (it *emptyIterator) Err() error      { return nil }
func (it *emptyIterator) Close() error    { return nil }
func (it *emptyIterator) IsDeleted() bool { return false }

// ============================================================
// LSMEngine Scan API
// ============================================================

// Scan returns an iterator over keys with the given prefix.
// Allocations: O(number_of_sources), not O(number_of_entries).
func (e *LSMEngine) Scan(prefix []byte) Iterator {
	fmt.Printf("[DEBUG-04] LSMEngine.Scan(): prefix=%q, memTable=%p\n", string(prefix), e.memTable)
	e.mu.RLock()
	defer e.mu.RUnlock()

	mi := NewMergeIterator(e, prefix)
	if len(mi.heap) == 0 && mi.err == nil {
		fmt.Printf("[DEBUG-04] LSMEngine.Scan(): heap is empty, returning emptyIterator\n")
		return &emptyIterator{}
	}
	fmt.Printf("[DEBUG-04] LSMEngine.Scan(): heap has %d sources, returning MVCCIterator\n", len(mi.heap))
	return NewMVCCIterator(mi)
}

// Ensure compile-time interface satisfaction
var _ Iterator = (*mergeIterator)(nil)
var _ Iterator = (*memtableIter)(nil)
var _ Iterator = (*sstableIter)(nil)
var _ Iterator = (*emptyIterator)(nil)
