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
// flushMemTable flushes all eligible shards' active MemTables into Level0
// SSTables WITHOUT holding e.mu.Lock during disk I/O.
//
// Previously this function held e.mu.Lock for the ENTIRE duration of writing
// every SSTable, blocking all Put/Delete on every shard for the whole flush.
// See: LSM-02, Глава XIII (flush must be non-blocking), Глава XIV (no locks
// during I/O).
//
// New flow:
//  1. Under a SHORT Lock, snapshot eligible shards (swap active → frozen) and
//     allocate each candidate a globally unique file number.
//  2. Release the Lock.
//  3. Write each SSTable to disk WITHOUT any engine lock (the slow part).
//  4. Under a SHORT Lock, apply the manifest edit and publish readers to
//     e.levels[0], then clear frozen pointers and release the arenas.
//
// The frozen MemTables are readable during the flush, so concurrent readers
// still see flushed data even before the SSTable is published.
//
//nolint:unused // flush goroutine worker
func (e *LSMEngine) flushMemTable() error {
	if e.manifest == nil {
		return fmt.Errorf("flushMemTable: engine manifest is nil")
	}
	if e.closed.Load() {
		return fmt.Errorf("flushMemTable: engine is closed")
	}

	// =========================================================================
	// Step 1: collect candidates + allocate file numbers under a SHORT lock.
	// =========================================================================
	type flushCandidate struct {
		shard   *Shard
		mt      *memtable.MemTable
		fileNum uint64
	}
	var candidates []flushCandidate
	{
		e.mu.Lock()
		nextFileNum := e.manifest.NextFileNum()
		for _, shard := range e.shards {
			if shard.memSizeLoad() == 0 {
				continue
			}
			// Mutate the shard's MemTable pointers under the SHARD's own mutex,
			// which also serialises writers (Shard.Put holds s.mu). Readers rely
			// on EBR for the active/frozen MemTable, so the pointer swap itself
			// must be consistent with the writer. See: LSM-02, ARENA-01.
			shard.mu.Lock()
			old := shard.memTable
			// Replace the active MemTable with a fresh one that owns its OWN
			// flat arena. Sharing the shard arena between the active and frozen
			// tables is unsafe: while we iterate the frozen table during flush,
			// new writes to the active table would land in the same 64 MB block
			// and could overwrite nodes the frozen table still references.
			// See: ARENA-01. The lock-free arena is never exhausted because the
			// 4 MB flush watermark is far below the 64 MB block.
			// See: HOT-01, PERF-01.
			shard.memTable = memtable.NewMemTable() // own arena, see ARENA-01
			shard.frozenMemTable = old
			atomic.StoreInt64(&shard.memSize, 0)
			shard.mu.Unlock()
			candidates = append(candidates, flushCandidate{shard: shard, mt: old, fileNum: nextFileNum})
			nextFileNum++
		}
		// Advance the manifest's file-number cursor so concurrent flushes and
		// compactions never collide. The manifest mutex serialises allocation.
		if len(candidates) > 0 {
			if err := e.manifest.Apply(&VersionEdit{NextFileNum: nextFileNum}); err != nil {
				logger.WarnComponent(logger.ComponentFlush, "flush: failed to advance manifest file-number cursor: %v", err)
			}
		}
		e.mu.Unlock()
	}

	if len(candidates) == 0 {
		return nil
	}

	// =========================================================================
	// Step 2: write each SSTable WITHOUT holding e.mu.Lock. This is the slow
	// disk I/O that previously blocked every writer on every shard.
	// =========================================================================
	var newReaders []*sstable.Reader
	for _, cand := range candidates {
		reader, err := e.flushOneMemTable(cand.mt, cand.fileNum)
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
		newReaders = append(newReaders, reader)
	}

	// =========================================================================
	// Step 3: publish to Level0 under a SHORT lock, clear frozen pointers, and
	// release the arenas immediately (instead of waiting for GC).
	// =========================================================================
	e.mu.Lock()
	for i, cand := range candidates {
		// Clear the frozen pointer AND release the arena under the shard's own
		// mutex so no concurrent reader (Shard.Get) is mid-iteration on a
		// MemTable whose arena is being reset. Previously Close() ran OUTSIDE
		// s.mu, racing a reader holding s.mu and reading the frozen table's
		// arena. See: LSM-02, ARENA-01, Глава XIV.
		cand.shard.mu.Lock()
		cand.shard.frozenMemTable = nil
		if cand.mt != nil {
			cand.mt.Close() // sl.Reset() -> arena.Reset() frees blocks, see SYMPTOM-03
		}
		// Publish the reader to BOTH the engine-global Level0 (legacy helpers,
		// compaction, merge iterators) AND the shard's own Level0 so Shard.Get
		// (which reads shard levels, never e.levels) can find the flushed data.
		// Without this the data vanishes from the reader's view after the frozen
		// MemTable is cleared. See: LSM-02, ARCH-01.
		cand.shard.levelsMu.Lock()
		cand.shard.levels[0] = append(cand.shard.levels[0], newReaders[i])
		cand.shard.levelsMu.Unlock()
		cand.shard.mu.Unlock()
		e.levels[0] = append(e.levels[0], newReaders[i])
	}
	e.mu.Unlock()

	return nil
}

// flushOneMemTable writes a single MemTable to a new Level0 SSTable and registers
// it in the manifest. It does NOT acquire e.mu.Lock; the caller must pass a
// globally unique fileNum (allocated under lock in flushMemTable) and must NOT
// hold e.mu.Lock while calling it, so the disk I/O happens outside the lock.
// The manifest edit is applied here (the manifest has its own mutex), and the
// resulting reader is returned for the caller to publish to e.levels[0].
func (e *LSMEngine) flushOneMemTable(mt *memtable.MemTable, fileNum uint64) (*sstable.Reader, error) {
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Create writer (currently using old API that works with os)
	writer, err := sstable.NewWriter(sstPath, mt.Size())
	if err != nil {
		return nil, fmt.Errorf("failed to create SSTable writer: %w", err)
	}

	// Iterate over all MemTable entries
	iter := mt.NewIterator()
	count := 0
	var minKey, maxKey []byte
	var first = true
	for iter.Next() {
		count++
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
		// The MemTable value is already in the tagged storage format (leading
		// type tag + payload). Pass it via AppendTagged so the writer preserves
		// the tag verbatim; a TypeValuePointer (large value) must survive to
		// the SSTable so reads can resolve it through the VLog.
		// See: WIS-KEY-01
		if err := writer.AppendTagged(key, value); err != nil {
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

	logger.Info(
		"flushOneMemTable: wrote %d entries to SSTable (fileNum=%d, path=%s)", count, fileNum, sstPath)

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

	// Apply edit to manifest. The manifest has its own mutex, so this is safe
	// to call WITHOUT e.mu.Lock. See: LSM-02, Глава XIV.
	if err := e.manifest.Apply(edit); err != nil {
		errors.CloseWithLog(reader, "flush-sstable")
		if err := e.vfs.Remove(sstPath); err != nil {
			logger.WarnComponent(logger.ComponentFlush, "flush: failed to remove %s: %v", sstPath, err)
		}
		return nil, fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	return reader, nil
}
