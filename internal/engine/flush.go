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
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

const (
	// MaxMemTableSize maximum MemTable size in bytes before flush
	MaxMemTableSize = 4 * 1024 * 1024 // 4 MB
	// MaxLevel0Files maximum number of files in Level0 before compaction
	MaxLevel0Files = 4
)

// flushMemTable flushes all shards' active MemTables into Level0 SSTables.
//
// With sharding, each shard owns an independent MemTable. This function drains
// every shard whose active MemTable exceeds the size threshold, swapping it to
// frozen (so concurrent writers get a fresh MemTable) and writing it to its own
// SSTable. All shards share the Level0 set and Manifest, so file numbers remain
// globally unique and reads can find flushed data in any shard's SSTable.
//
// The caller (flushWorker) holds no lock; this function acquires e.mu.Lock once
// to atomically snapshot and swap all eligible shard MemTables.
//
//nolint:unused // flush goroutine worker
func (e *LSMEngine) flushMemTable() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Collect eligible shards under the lock: swap each shard's active MemTable
	// to frozen and reset its size watermark. Writers acquiring RLock after this
	// point write to the fresh MemTable, never the frozen one being flushed.
	// See: SYMPTOM-03, HOT-01
	type flushCandidate struct {
		shard *Shard
		mt    *memtable.MemTable
	}
	var candidates []flushCandidate
	for _, shard := range e.shards {
		if shard.memSizeLoad() == 0 {
			continue
		}
		old := shard.memTable
		shard.memTable = memtable.NewMemTable()
		shard.frozenMemTable = old
		atomic.StoreInt64(&shard.memSize, 0)
		candidates = append(candidates, flushCandidate{shard: shard, mt: old})
	}

	if len(candidates) == 0 {
		return nil
	}

	// Flush each candidate MemTable into its own Level0 SSTable.
	// Readers opened here are appended to the shared e.levels[0]; manifest file
	// numbers are allocated sequentially to remain unique across shards.
	nextFileNum := e.manifest.NextFileNum()
	var newReaders []*sstable.Reader
	for _, cand := range candidates {
		reader, err := e.flushOneMemTable(cand.mt, nextFileNum)
		if err != nil {
			// On failure, clear the frozen pointer we swapped in and put the
			// MemTable back so its data is not lost.
			cand.shard.frozenMemTable = nil
			// Close any readers already produced this round.
			for _, r := range newReaders {
				errors.CloseWithLog(r, "flush-new-sstable")
			}
			return err
		}
		nextFileNum++
		newReaders = append(newReaders, reader)
	}

	// Successfully flushed — release the MemTable's arena immediately (instead
	// of waiting for GC), then clear frozen pointers and publish to Level0.
	// Close() calls sl.Reset() -> arena.Reset(), which drops all blocks so the
	// GC can reclaim them. Previously the arena blocks (512 MB each) were held
	// until a later GC, causing OOM across many flush cycles. See: SYMPTOM-03
	for i, cand := range candidates {
		if cand.mt != nil {
			cand.mt.Close()
		}
		cand.shard.frozenMemTable = nil
		e.levels[0] = append(e.levels[0], newReaders[i])
	}

	return nil
}

// flushOneMemTable writes a single MemTable to a new Level0 SSTable and registers
// it in the manifest. Caller must hold e.mu.Lock (flushMemTable holds it) and pass
// a globally unique fileNum (allocated in flushMemTable).
func (e *LSMEngine) flushOneMemTable(mt *memtable.MemTable, fileNum uint64) (*sstable.Reader, error) {
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Create writer (currently using old API that works with os)
	writer, err := sstable.NewWriter(sstPath, mt.Size())
	if err != nil {
		return nil, fmt.Errorf("failed to create SSTable writer: %w", err)
	}

	// Iterate over all MemTable entries
	iter := mt.NewIterator()
	var minKey, maxKey []byte
	var first = true
	for iter.Next() {
		key, value := iter.Key(), iter.Value()
		// For range filter we need user keys (without timestamp)
		userKey := key.Key
		if first {
			minKey = make([]byte, len(userKey))
			copy(minKey, userKey)
			maxKey = make([]byte, len(userKey))
			copy(maxKey, userKey)
			first = false
		} else {
			if bytes.Compare(userKey, minKey) < 0 {
				minKey = userKey
			}
			if bytes.Compare(userKey, maxKey) > 0 {
				maxKey = userKey
			}
		}
		if err := writer.Append(key, value); err != nil {
			writer = nil
			// Delete partially written file via VFS
			if err := e.vfs.Remove(sstPath); err != nil {
				logger.WarnComponent(logger.ComponentFlush, "flush: failed to remove %s: %v", sstPath, err)
			}
			return nil, fmt.Errorf("failed to append key to SSTable: %w", err)
		}
	}
	iter.Close()

	if err := writer.Finish(); err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			logger.WarnComponent(logger.ComponentFlush, "flush: failed to remove %s: %v", sstPath, err)
		}
		return nil, fmt.Errorf("failed to finish SSTable: %w", err)
	}

	// Open the created SSTable for reading
	reader, err := sstable.Open(sstPath)
	if err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			logger.WarnComponent(logger.ComponentFlush, "flush: failed to remove %s: %v", sstPath, err)
		}
		return nil, fmt.Errorf("failed to open SSTable: %w", err)
	}

	// Get file size
	stat, err := e.vfs.Stat(sstPath)
	if err != nil {
		errors.CloseWithLog(reader, "flush-sstable")
		if err := e.vfs.Remove(sstPath); err != nil {
			logger.WarnComponent(logger.ComponentFlush, "flush: failed to remove %s: %v", sstPath, err)
		}
		return nil, fmt.Errorf("failed to stat SSTable: %w", err)
	}

	// Create VersionEdit to add new file. LastTS persists the highest committed
	// timestamp so it survives restart even after the WAL is truncated.
	// See: ARCH-07.
	edit := &VersionEdit{
		NewFiles: []SSTableInfo{
			{
				FileNum: fileNum,
				Level:   0,
				MinKey:  minKey,
				MaxKey:  maxKey,
				Size:    uint64(stat.Size()),
			},
		},
		NextFileNum: fileNum + 1,
		LastTS:      atomic.LoadUint64(&e.LastTS),
	}

	// Apply edit to manifest
	if err := e.manifest.Apply(edit); err != nil {
		errors.CloseWithLog(reader, "flush-sstable")
		if err := e.vfs.Remove(sstPath); err != nil {
			logger.WarnComponent(logger.ComponentFlush, "flush: failed to remove %s: %v", sstPath, err)
		}
		return nil, fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	return reader, nil
}
