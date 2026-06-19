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
	"log"

	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// Iterator is the public iterator interface.
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Err() error
	Close() error
}

// engineIteratorAdapter adapts sstable.Iterator to engine.Iterator.
type engineIteratorAdapter struct {
	inner  sstable.Iterator
	prefix []byte
}

func (it *engineIteratorAdapter) Next() bool {
	for it.inner.Next() {
		if bytes.HasPrefix(it.inner.Key().Key, it.prefix) {
			return true
		}
	}
	return false
}

func (it *engineIteratorAdapter) Key() []byte {
	return it.inner.Key().Key
}

func (it *engineIteratorAdapter) Value() []byte {
	return it.inner.Value()
}

func (it *engineIteratorAdapter) Err() error {
	return nil
}

func (it *engineIteratorAdapter) Close() error {
	it.inner.Close()
	return nil
}

// memTableIteratorAdapter adapts MemTableIterator to sstable.Iterator.
type memTableIteratorAdapter struct {
	inner *MemTableIterator
}

func (it *memTableIteratorAdapter) Next() bool {
	return it.inner.Next()
}

func (it *memTableIteratorAdapter) Key() mvcc.MVCCKey {
	return it.inner.Key()
}

func (it *memTableIteratorAdapter) Value() []byte {
	return it.inner.Value()
}

func (it *memTableIteratorAdapter) Close() {
	it.inner.Close()
}

// emptyIterator returns false immediately.
type emptyIterator struct{}

func (it *emptyIterator) Next() bool    { return false }
func (it *emptyIterator) Key() []byte   { return nil }
func (it *emptyIterator) Value() []byte { return nil }
func (it *emptyIterator) Err() error    { return nil }
func (it *emptyIterator) Close() error  { return nil }

// Scan returns an iterator over keys with the given prefix.
func (e *LSMEngine) Scan(prefix []byte) Iterator {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var iters []sstable.Iterator

	if e.memTable != nil {
		iters = append(iters, &memTableIteratorAdapter{inner: e.memTable.NewIterator()})
	}

	if e.frozenMemTable != nil {
		iters = append(iters, &memTableIteratorAdapter{inner: e.frozenMemTable.NewIterator()})
	}

	for _, level := range e.levels {
		for _, sst := range level {
			sstIter, err := sst.NewIterator()
			if err == nil {
				iters = append(iters, sstIter)
			} else {
				log.Printf("[Scan] failed to create iterator for SSTable: %v", err)
			}
		}
	}

	if len(iters) == 0 {
		return &emptyIterator{}
	}

	mergeIter := sstable.NewMergeIterator(iters)
	return &engineIteratorAdapter{
		inner:  mergeIter,
		prefix: prefix,
	}
}
