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

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// Iterator is a generic interface for iterating over key-value pairs.
type Iterator interface {
	Next() bool
	Key() mvcc.MVCCKey
	Value() []byte
	Close()
}

// MergeIterator merges multiple iterators into a single sorted stream.
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
	ki, kj := h[i].key, h[j].key
	cmp := keys.CompareKeys(ki.Key, kj.Key)
	if cmp < 0 {
		return true
	}
	if cmp > 0 {
		return false
	}
	return ki.Timestamp > kj.Timestamp
}
func (h mergeHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *mergeHeap) Push(x interface{}) {
	*h = append(*h, x.(*heapItem)) //nolint:errcheck
}

func (h *mergeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// NewMergeIterator creates a new merge iterator.
func NewMergeIterator(iters []Iterator) *MergeIterator {
	h := &mergeHeap{}
	heap.Init(h)

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
func (mi *MergeIterator) Next() bool {
	if mi.closed {
		return false
	}

	mi.current = nil

	for mi.heap.Len() > 0 {
		h := mi.heap
		item := (*h)[0]
		userKey := item.key.Key
		bestItem := item

		var sameKeyItems []*heapItem
		for mi.heap.Len() > 0 && keys.CompareKeys((*mi.heap)[0].key.Key, userKey) == 0 {
			popped := heap.Pop(mi.heap)
			it, ok := popped.(*heapItem)
			if !ok {
				mi.closed = true
				return false
			}
			sameKeyItems = append(sameKeyItems, it)
			if it.key.Timestamp > bestItem.key.Timestamp {
				bestItem = it
			}
		}

		for _, it := range sameKeyItems {
			if it == bestItem {
				continue
			}
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

		if len(bestItem.val) == 0 {
			if bestItem.iter.Next() {
				heap.Push(mi.heap, &heapItem{
					iter: bestItem.iter,
					key:  bestItem.iter.Key(),
					val:  bestItem.iter.Value(),
				})
			} else {
				bestItem.iter.Close()
			}
			continue
		}

		mi.current = bestItem

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

	mi.closed = true
	return false
}

// Key returns the current key.
func (mi *MergeIterator) Key() mvcc.MVCCKey {
	if mi.current == nil {
		panic("Key called before Next or after exhaustion")
	}
	return mi.current.key
}

// Value returns the current value.
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

	for mi.heap.Len() > 0 {
		popped := heap.Pop(mi.heap)
		if it, ok := popped.(*heapItem); ok {
			it.iter.Close()
		}
	}

	if mi.current != nil {
		mi.current.iter.Close()
		mi.current = nil
	}
}
