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
	"fmt"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// CollectLiveValuePointers collects all live ValuePointers from the LSM tree.
func (e *LSMEngine) CollectLiveValuePointers() (map[ValuePointer]struct{}, error) {
	if e.closed.Load() {
		return nil, fmt.Errorf("engine closed")
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	livePointers := make(map[ValuePointer]struct{})
	processValue := func(value []byte) {
		if len(value) == ValuePointerSize {
			if vp, ok := DecodeValuePointer(value); ok {
				livePointers[vp] = struct{}{}
			}
		}
	}
	// Collect live pointers from every shard's active and frozen MemTable.
	// See: HOT-01
	for _, shard := range e.shards {
		if shard.memTable != nil {
			iter := shard.memTable.NewIterator()
			for iter.Next() {
				processValue(iter.Value())
			}
			iter.Close()
		}
		if shard.frozenMemTable != nil {
			iter := shard.frozenMemTable.NewIterator()
			for iter.Next() {
				processValue(iter.Value())
			}
			iter.Close()
		}
	}
	for level := 0; level < len(e.levels); level++ {
		for _, reader := range e.levels[level] {
			iter, err := reader.NewIterator()
			if err != nil {
				logger.Warn("gc: failed to create iterator for SSTable: %v", err)
				continue
			}
			for iter.Next() {
				processValue(iter.Value())
			}
			iter.Close()
		}
	}
	return livePointers, nil
}

// InvalidateVLogPointers removes invalid VLog pointers from MemTables.
func (e *LSMEngine) InvalidateVLogPointers() {
	if e.closed.Load() {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	processTable := func(mt *memtable.MemTable) {
		iter := mt.NewIterator()
		defer iter.Close()
		var toDelete []mvcc.MVCCKey
		for iter.Next() {
			val := iter.Value()
			if len(val) == 12 {
				toDelete = append(toDelete, iter.Key())
			}
		}
		for _, key := range toDelete {
			mt.DeleteWithTS(key)
		}
	}
	// Invalidate pointers in every shard's active and frozen MemTable.
	// See: HOT-01
	for _, shard := range e.shards {
		if shard.memTable != nil {
			processTable(shard.memTable)
		}
		if shard.frozenMemTable != nil {
			processTable(shard.frozenMemTable)
		}
	}
}
