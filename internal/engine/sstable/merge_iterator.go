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

package sstable

import (
	"container/heap"
	"scoriadb/internal/mvcc"
)

// Iterator is a generic interface for iterating over key-value pairs.
type Iterator interface {
	Next() bool
	Key() mvcc.MVCCKey
	Value() []byte
	Close()
}

// MergeIterator merges multiple iterators into a single sorted stream.
// It uses a min-heap to select the smallest key at each step.
// Duplicate user keys are collapsed: only the latest version (highest timestamp)
// is yielded, and tombstones (empty values) are skipped.
type MergeIterator struct {
	heap    *mergeHeap
	current *heapItem
	closed  bool
}

type heapItem struct {
	iter Iterator
	key  mvcc.MVCCKey
	val  []byte
}

type mergeHeap []*heapItem

func (h mergeHeap) Len() int { return len(h) }
func (h mergeHeap) Less(i, j int) bool {
	// Compare keys: first by user key lexicographically, then by timestamp descending
	// (newer timestamps first because we want to keep the latest version).
	ki, kj := h[i].key, h[j].key
	cmp := compareKeys(ki.Key, kj.Key)
	if cmp < 0 {
		return true
	}
	if cmp > 0 {
		return false
	}
	// Same user key: higher timestamp (newer) comes first
	return ki.Timestamp > kj.Timestamp
}
func (h mergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *mergeHeap) Push(x interface{}) {
	*h = append(*h, x.(*heapItem))
}

func (h *mergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// NewMergeIterator creates a new merge iterator over the given iterators.
// All iterators must be sorted by key (as SSTable iterators are).
// The merge iterator yields keys in sorted order, deduplicating by user key
// and timestamp according to the merge logic (caller decides which version to keep).
func NewMergeIterator(iters []Iterator) *MergeIterator {
	h := &mergeHeap{}
	heap.Init(h)

	// Initialize heap with the first element from each iterator
	for _, iter := range iters {
		if iter.Next() {
			heap.Push(h, &heapItem{
				iter: iter,
				key:  iter.Key(),
				val:  iter.Value(),
			})
		} else {
			iter.Close()
		}
	}

	return &MergeIterator{
		heap: h,
	}
}

// Next advances the iterator to the next key.
// It skips duplicate user keys (keeping the latest version) and tombstones.
func (mi *MergeIterator) Next() bool {
	if mi.closed {
		return false
	}

	// Clear current
	mi.current = nil

	// Keep pulling items from heap until we find a key to yield
	for mi.heap.Len() > 0 {
		// Peek at the smallest item (heap root)
		h := mi.heap
		item := (*h)[0]
		userKey := item.key.Key
		bestItem := item
		// Pop all items with the same user key, keeping the one with highest timestamp
		// (since heap is ordered by user key then timestamp descending, the first item
		// is already the latest version for that user key).
		// However, there may be multiple versions across different iterators.
		// We'll pop all items with same user key, track the latest.
		var sameKeyItems []*heapItem
		for mi.heap.Len() > 0 && compareKeys((*mi.heap)[0].key.Key, userKey) == 0 {
			it := heap.Pop(mi.heap).(*heapItem)
			sameKeyItems = append(sameKeyItems, it)
			// Keep the one with highest timestamp (already sorted by heap)
			if it.key.Timestamp > bestItem.key.Timestamp {
				bestItem = it
			}
		}

		// Advance iterators of the popped items (except the one we keep)
		for _, it := range sameKeyItems {
			if it == bestItem {
				continue
			}
			// Advance iterator and push back if more entries
			if it.iter.Next() {
				heap.Push(mi.heap, &heapItem{
					iter: it.iter,
					key:  it.iter.Key(),
					val:  it.iter.Value(),
				})
			} else {
				it.iter.Close()
			}
		}

		// Now handle the best item (latest version)
		// Skip tombstone (empty value)
		if len(bestItem.val) == 0 {
			// Tombstone: discard this version, do not yield
			// Advance its iterator and push back if more entries
			if bestItem.iter.Next() {
				heap.Push(mi.heap, &heapItem{
					iter: bestItem.iter,
					key:  bestItem.iter.Key(),
					val:  bestItem.iter.Value(),
				})
			} else {
				bestItem.iter.Close()
			}
			continue // look for next key
		}

		// Yield this key-value pair
		mi.current = bestItem
		// Advance its iterator and push back if more entries
		if bestItem.iter.Next() {
			heap.Push(mi.heap, &heapItem{
				iter: bestItem.iter,
				key:  bestItem.iter.Key(),
				val:  bestItem.iter.Value(),
			})
		} else {
			bestItem.iter.Close()
		}
		return true
	}

	// No more items
	mi.closed = true
	return false
}

// Key returns the current key. Must be called after a successful Next().
func (mi *MergeIterator) Key() mvcc.MVCCKey {
	if mi.current == nil {
		panic("Key called before Next or after exhaustion")
	}
	return mi.current.key
}

// Value returns the current value. Must be called after a successful Next().
func (mi *MergeIterator) Value() []byte {
	if mi.current == nil {
		panic("Value called before Next or after exhaustion")
	}
	return mi.current.val
}

// Close closes all underlying iterators.
func (mi *MergeIterator) Close() {
	if mi.closed {
		return
	}
	mi.closed = true
	// Close all iterators still in heap
	for mi.heap.Len() > 0 {
		item := heap.Pop(mi.heap).(*heapItem)
		item.iter.Close()
	}
	// Close current iterator if any
	if mi.current != nil {
		mi.current.iter.Close()
		mi.current = nil
	}
}
