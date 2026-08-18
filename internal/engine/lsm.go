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
	"container/list"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// LSMEngine is the main LSM tree engine.
//
// Writes are sharded across multiple MemTables (see Shard) to eliminate the
// single-SkipList-mutex contention that serialized all PUTs. Each shard owns an
// independent active MemTable (and thus its own SkipList mutex), so concurrent
// writes with different keys scale near-linearly with the number of cores.
//
// All shards share the WAL, Manifest, VLog, and the SSTable level set. Reads
// scan every shard's active/frozen MemTable plus the shared SSTables.
type LSMEngine struct {
	mu          sync.RWMutex
	dataDir     string
	shards      []*Shard // independent write partitions
	vlog        *VLogImpl
	wal         *WAL
	unifiedMmap *UnifiedMmap // unified mmap ring buffer (hot path)
	manifest    *Manifest
	vfs         vfs.VFS
	// levels is the shared LSM SSTable level set. Invariant: every read of
	// e.levels must happen under e.mu.RLock, and every mutation (flushMemTable
	// append, compactLevel0 clear/append) under e.mu.Lock. Readers must snapshot
	// the slices under RLock (see Scan / NewMergeIterator) and never hold pointers
	// into the live slices after releasing the lock.
	levels [][]*sstable.Reader
	LastTS uint64
	// snapshotRegistry tracks active MVCC snapshots with reference counting.
	// Replaces the previous single-value minActiveSnapshotTS field.
	snapshotRegistry *snapshotRegistry
	closed           atomic.Bool
	lastCommitCache  map[string]uint64
	cacheMu          sync.RWMutex

	// lastCommitCacheMap and lastCommitCacheLRU implement a bounded LRU cache
	// for the last-commit-timestamp lookup. See: DEF-D3.
	lastCommitCacheMap map[string]*list.Element
	lastCommitCacheLRU *list.List

	// Background tasks
	flushCh     chan struct{}
	compactCh   chan struct{}
	stopCh      chan struct{}
	wg          sync.WaitGroup
	flushTicker *time.Ticker
}

// NewLSMEngine creates a new LSM engine.
func NewLSMEngine(dataDir string, opts ...WALOptions) (*LSMEngine, error) {
	vfs := vfs.NewDefaultVFS()

	if err := vfs.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	manifestPath := filepath.Join(dataDir, "MANIFEST")
	manifest, err := NewManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest: %w", err)
	}

	vlogPath := filepath.Join(dataDir, "vlog.db")
	vlog, err := OpenVLog(vfs, vlogPath)
	if err != nil {
		errors.CloseWithLog(manifest, "manifest")
		return nil, fmt.Errorf("failed to open vlog: %w", err)
	}

	walOpts := DefaultWALOptions()
	if len(opts) > 0 {
		walOpts = opts[0]
	}
	shardCount := DefaultShardCount()
	if shardCount < 1 {
		shardCount = 1
	}

	// Open unified mmap ring buffer (hot write path)
	unifiedPath := filepath.Join(dataDir, "data.mmap")
	unifiedMmap, err := OpenUnifiedMmap(unifiedPath)
	if err != nil {
		errors.CloseWithLog(vlog, "vlog")
		errors.CloseWithLog(manifest, "manifest")
		return nil, fmt.Errorf("failed to open unified mmap: %w", err)
	}

	// Open one WAL per shard. Each shard gets its own WAL file (wal_<id>.log)
	// with its own mutex and group-commit writer, removing the single-WAL
	// serialization point that otherwise caps write throughput regardless of
	// core count. See: HOT-01
	shardWALs := make([]*WAL, shardCount)
	for i := 0; i < shardCount; i++ {
		walPath := filepath.Join(dataDir, fmt.Sprintf("wal_%d.log", i))
		var wal *WAL
		if walOpts.GroupCommitEnabled {
			wal, err = OpenWALWithOptions(walPath, walOpts)
		} else {
			wal, err = OpenWAL(walPath)
		}
		if err != nil {
			errors.CloseWithLog(unifiedMmap, "unified-mmap")
			errors.CloseWithLog(vlog, "vlog")
			errors.CloseWithLog(manifest, "manifest")
			for j := 0; j < i; j++ {
				errors.CloseWithLog(shardWALs[j], "wal")
			}
			return nil, fmt.Errorf("failed to open wal for shard %d: %w", i, err)
		}
		shardWALs[i] = wal
	}

	// Seed timestamp monotonicity from the manifest first (persisted LastTS),
	// then raise it further from the highest commitTS found in any shard WAL.
	// See: ARCH-07.
	lastTS := manifest.LastTS()
	if lastTS < 1 {
		lastTS = 1
	}

	levels := make([][]*sstable.Reader, 10)
	manifestLevels := manifest.GetLevels()
	for level, infos := range manifestLevels {
		if level >= len(levels) {
			continue
		}
		for _, info := range infos {
			sstPath := filepath.Join(dataDir, fmt.Sprintf("%06d.sst", info.FileNum))
			reader, err := sstable.Open(sstPath)
			if err != nil {
				continue
			}
			levels[level] = append(levels[level], reader)
		}
	}

	engine := &LSMEngine{
		dataDir:            dataDir,
		shards:             make([]*Shard, shardCount),
		vlog:               vlog,
		unifiedMmap:        unifiedMmap,
		manifest:           manifest,
		vfs:                vfs,
		levels:             levels,
		LastTS:             lastTS,
		lastCommitCache:    make(map[string]uint64),
		lastCommitCacheMap: make(map[string]*list.Element),
		lastCommitCacheLRU: list.New(),
		snapshotRegistry:   newSnapshotRegistry(),
	}

	// Create one shard per core. Each shard owns an independent MemTable (and
	// therefore an independent SkipList mutex) and an independent WAL (with its
	// own mutex and group-commit writer), enabling concurrent writes to scale
	// across cores. See: HOT-01
	for i := range engine.shards {
		shard, err := NewShard(i, dataDir, walOpts)
		if err != nil {
			errors.CloseWithLog(engine, "engine")
			return nil, fmt.Errorf("shard %d: %w", i, err)
		}
		engine.shards[i] = shard
		engine.shards[i].wal = shardWALs[i]
	}
	// Backward-compatible handle: e.wal points to shard 0's WAL. Kept for
	// callers (e.g. benchmarks/tests) that flush a single WAL; production
	// writes go through the shard-local WAL.
	engine.wal = shardWALs[0]

	engine.InvalidateVLogPointers()
	// Recover each shard's WAL into that shard's MemTable. Each shard only
	// replayed entries that were routed to it, so recovery preserves the
	// sharding layout across restarts. See: REC-01, HOT-01
	// Track the maximum commitTS across all shards and raise LastTS so new
	// timestamps continue to be strictly monotonic and unique after restart.
	var walMaxTS uint64
	for _, shard := range engine.shards {
		maxTS, err := recoverFromWAL(shard.wal, shard.memTable, engine.vlog, shard.id, engine.shardIndex)
		if err != nil {
			errors.CloseWithLog(engine, "engine")
			return nil, fmt.Errorf("failed to recover from wal for shard %d: %w", shard.id, err)
		}
		if maxTS > walMaxTS {
			walMaxTS = maxTS
		}
	}
	if walMaxTS > engine.LastTS {
		engine.LastTS = walMaxTS
	}

	// Start background tasks
	engine.startBackgroundTasks()

	return engine, nil
}

// shardIndex returns the shard responsible for key (O(1) hash).
func (e *LSMEngine) shardIndex(key []byte) int {
	if len(e.shards) == 0 {
		return 0
	}
	return int(hashKey(key) % uint64(len(e.shards)))
}

// shard returns the shard responsible for key.
func (e *LSMEngine) shard(key []byte) *Shard {
	return e.shards[e.shardIndex(key)]
}

// DefaultShardCount returns the number of shards to create: one per CPU core,
// capped at 16 to bound resource usage on very large machines. See: HOT-01.
func DefaultShardCount() int {
	n := runtime.GOMAXPROCS(0)
	if n > 16 {
		return 16
	}
	if n < 1 {
		return 1
	}
	return n
}

// hashKey is a fast, allocation-free hash used to route a key to a shard.
func hashKey(key []byte) uint64 {
	const (
		offset64 = 1469598103934665603
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for _, b := range key {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// startBackgroundTasks starts background flush and compaction workers.
func (e *LSMEngine) startBackgroundTasks() {
	e.flushCh = make(chan struct{}, 1)
	e.compactCh = make(chan struct{}, 1)
	e.stopCh = make(chan struct{})
	e.flushTicker = time.NewTicker(10 * time.Second)

	// Flush worker
	e.wg.Add(1)
	go e.flushWorker()

	// Compaction worker
	e.wg.Add(1)
	go e.compactionWorker()

	logger.Info("background tasks started (flush + compaction)")
}

// stopBackgroundTasks stops background workers.
func (e *LSMEngine) stopBackgroundTasks() {
	if e.stopCh != nil {
		close(e.stopCh)
	}
	if e.flushTicker != nil {
		e.flushTicker.Stop()
	}
	e.wg.Wait()
	logger.Info("background tasks stopped")
}

// flushWorker periodically checks and flushes MemTable.
func (e *LSMEngine) flushWorker() {
	defer e.wg.Done()
	for {
		select {
		case <-e.stopCh:
			return
		case <-e.flushTicker.C:
			// Check every shard's MemTable size. We trigger a flush when the
			// shard's arena has grown past MaxMemTableSize OR when the logical
			// byte counter crosses the watermark. The arena is now block-aligned
			// at 4 MB, so SizeBytes reflects real occupancy and prevents the
			// arena from silently holding hundreds of MB before a flush.
			// See: SYMPTOM-02, SYMPTOM-03, HOT-01, PERF-01
			e.mu.RLock()
			for _, shard := range e.shards {
				mt := shard.memTable
				if mt == nil {
					continue
				}
				if shard.memSizeLoad() > MaxMemTableSize ||
					int64(mt.SizeBytes()) > MaxMemTableSize {
					e.mu.RUnlock()
					select {
					case e.flushCh <- struct{}{}:
					default:
					}
					break
				}
			}
			e.mu.RUnlock()
		case <-e.flushCh:
			if err := e.flushMemTable(); err != nil {
				logger.Warn("flush failed: %v", err)
			}
		}
	}
}

// compactionWorker periodically checks and triggers compaction.
func (e *LSMEngine) compactionWorker() {
	defer e.wg.Done()
	compTicker := time.NewTicker(30 * time.Second)
	defer compTicker.Stop()

	for {
		select {
		case <-e.stopCh:
			return
		case <-compTicker.C:
			e.mu.RLock()
			level0Count := len(e.levels[0])
			e.mu.RUnlock()

			if level0Count > MaxLevel0Files {
				select {
				case e.compactCh <- struct{}{}:
				default:
				}
			}
		case <-e.compactCh:
			e.maybeCompact()
		}
	}
}

// NextTimestamp returns a new unique timestamp.
func (e *LSMEngine) NextTimestamp() uint64 {
	return atomic.AddUint64(&e.LastTS, 1)
}

// PutWithTS writes a key-value pair with the given commit timestamp.
//
// OPTIMIZATION v0.3.1: Unified MMap Hot Path
//   - Single write to unified mmap ring buffer (replaces WAL + VLog dual write)
//   - Zero-copy unsafe memcpy (no bounds checking)
//   - Atomic offset reservation (no mutex in hot path)
//   - For large values (>MaxInlineSize), writes full value to unified mmap,
//     stores 12-byte ValuePointer in MemTable only
//
// Zero allocations in hot path:
//   - Stack-allocated [12]byte for ValuePointer encoding
//   - Direct mmap write via unsafe pointer arithmetic
//   - Ring buffer for skip list nodes
func (e *LSMEngine) PutWithTS(key, value []byte, commitTS uint64) error {
	// Fast closed check with atomic — no mutex
	if e.closed.Load() {
		return fmt.Errorf("engine closed")
	}

	var vp ValuePointer
	var inlineValue []byte
	var isLarge bool

	// Determine storage strategy based on value size
	if len(value) <= MaxInlineSize {
		inlineValue = value
		isLarge = false
	} else {
		// Large value: write to unified mmap (single write, no VLog call)
		// The unified mmap uses atomic.AddUint64 — no mutex in hot path
		offset, err := e.unifiedMmap.WriteEntry(OpPut, key, value, commitTS)
		if err != nil {
			return fmt.Errorf("failed to write to unified mmap: %w", err)
		}
		vp = ValuePointer{Offset: int64(offset), Size: int32(len(value))}
		isLarge = true
	}

	mvccKey := mvcc.NewMVCCKey(key, commitTS)

	// Encode stored value with a leading type tag (v0.4+). The tag byte
	// disambiguates a real ValuePointer from a user value of exactly 12 bytes.
	//
	// Large values carry tag TypeValuePointer + 12-byte ValuePointer.
	// Inline values carry tag TypeInline + raw bytes.
	var vpBuf [ValuePointerSize]byte
	var storedValue []byte
	if isLarge {
		EncodeValuePointer(vp, vpBuf[:])
		// Use unsafe to create slice without causing vpBuf to escape to heap.
		// The slice points to the stack-allocated array, and Put() copies the
		// data into the arena, so the stack lifetime is sufficient.
		var tagged [1 + ValuePointerSize]byte
		tagged[0] = TypeValuePointer
		copy(tagged[1:], vpBuf[:])
		storedValue = tagged[:]
	} else {
		// Small inline value (<= MaxInlineSize, default 64). Tag on a stack
		// buffer so the hot path stays allocation-free. MaxInlineSize is a var
		// (raised temporarily in benchmarks); fall back to a heap slice only if
		// it grows beyond this fixed stack budget.
		const maxInlineStack = 1 + 64
		if len(inlineValue) <= maxInlineStack-1 {
			var tagged [maxInlineStack]byte
			tagged[0] = TypeInline
			copy(tagged[1:], inlineValue)
			storedValue = tagged[:1+len(inlineValue)]
		} else {
			storedValue = make([]byte, 1+len(inlineValue))
			storedValue[0] = TypeInline
			copy(storedValue[1:], inlineValue)
		}
	}

	// Route this key to its shard. The shard owns both the WAL and the MemTable,
	// so the WAL write and the MemTable Put hit the SAME shard, never a global
	// lock. Concurrent writes with different keys touch different shard WALs
	// (different mutexes) AND different SkipList mutexes — removing both global
	// serialization points. See: HOT-01
	shard := e.shard(key)

	// For small values: write to the shard-local WAL (still needed for durability)
	// For large values: WAL write is ELIMINATED — data is already in unified mmap
	if !isLarge {
		walEntry := newWalEntry()
		walEntry.Op = OpPut
		walEntry.Key = key
		// WAL stores the raw (untagged) user value. Recovery re-tags it on replay,
		// so the WAL never double-tags.
		walEntry.Value = inlineValue
		walEntry.Timestamp = commitTS
		walEntry.IsLarge = false

		if err := shard.wal.Write(walEntry); err != nil {
			putWalEntry(walEntry)
			return fmt.Errorf("failed to write to wal: %w", err)
		}
		putWalEntry(walEntry)
	}

	// CRITICAL: Protect memTable access with RLock to prevent race with flushMemTable.
	// flushMemTable holds Lock (exclusive) while swapping a shard's memTable.
	// RLock ensures we see a stable pointer — either the old MemTable (before swap)
	// or the new one (after swap), never a half-swapped state.
	// WAL write is done BEFORE the lock to avoid I/O under RLock.
	// See: SYMPTOM-01, SYMPTOM-03, HOT-01
	e.mu.RLock()
	shard.memTable.Put(mvccKey, storedValue)
	e.mu.RUnlock()
	atomic.AddInt64(&shard.memSize, int64(len(key)+len(value)))
	e.updateLastCommitCache(key, commitTS)

	return nil
}

// WriteAtomicBatch writes an atomic batch of operations.
func (e *LSMEngine) WriteAtomicBatch(data []byte, commitTS uint64) error {
	if e.closed.Load() {
		return fmt.Errorf("engine closed")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	// Decode ops once to determine which shards are affected by this batch.
	ops, err := decodeBatchLocal(data)
	if err != nil {
		return fmt.Errorf("failed to decode batch: %w", err)
	}

	// Write the full batch to each affected shard's WAL. A batch may span
	// multiple shards; writing it to every affected shard's WAL guarantees that
	// recovery replays the whole batch into the correct shards.
	// See: HOT-01, REC-01
	affected := make(map[int]struct{}, len(ops))
	for _, op := range ops {
		affected[e.shardIndex(op.Key)] = struct{}{}
	}
	for idx := range affected {
		walEntry := newWalEntry()
		walEntry.Op = OpBatch
		walEntry.Key = nil
		walEntry.Value = data
		walEntry.Timestamp = commitTS
		walEntry.IsLarge = false
		if err := e.shards[idx].wal.Write(walEntry); err != nil {
			putWalEntry(walEntry)
			return fmt.Errorf("failed to write batch to wal: %w", err)
		}
		putWalEntry(walEntry)
	}

	// Track memSize growth per shard so each shard's watermark stays accurate
	// for the flush worker. Batch path is cold (already allocates during decode),
	// so a small map is acceptable here. See: HOT-01, PERF-01
	shardDeltas := make(map[int]int64, len(ops))
	for _, op := range ops {
		mvccKey := mvcc.NewMVCCKey(op.Key, commitTS)
		shard := e.shard(op.Key)
		if op.IsDelete {
			// CRITICAL: Delete requires both Put(nil) AND Delete() to set deleted flag.
			// DeleteWithTS ensures tombstone is correctly marked as deleted.
			// See: PROMPT-TOMBSTONE-BATCH-FIX
			shard.memTable.DeleteWithTS(mvccKey)
		} else {
			shard.memTable.Put(mvccKey, tagStoredValue(op.Value))
			shardDeltas[e.shardIndex(op.Key)] += int64(len(op.Key) + len(op.Value))
		}
		e.updateLastCommitCache(op.Key, commitTS)
	}
	for idx, delta := range shardDeltas {
		atomic.AddInt64(&e.shards[idx].memSize, delta)
	}
	return nil
}

// GetWithTS reads a value with the given snapshot timestamp.
func (e *LSMEngine) GetWithTS(key []byte, snapshotTS uint64) ([]byte, error) {
	if e.closed.Load() {
		return nil, fmt.Errorf("engine closed")
	}

	// CRITICAL: Copy MemTable pointers under RLock, then release lock before SSTable scan.
	// Holding RLock during SSTable scan starves flushMemTable (needs Lock).
	// This also prevents the transaction hang (SYMPTOM-04) where 16 goroutines
	// hold RLock while iterating many SSTables.
	// A key may live in any shard's active or frozen MemTable, so we scan all of them.
	// See: SYMPTOM-04, HOT-01
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)

	e.mu.RLock()
	for _, shard := range e.shards {
		mt := shard.memTable
		if mt == nil {
			continue
		}
		if val, found := mt.Get(mvccKey); found {
			e.mu.RUnlock()
			decoded, err := e.decodeStoredValue(val, false)
			return decoded, err
		}
	}
	e.mu.RUnlock()

	// Scan SSTables under RLock — this is fast (index lookup, not full scan).
	// If this becomes a bottleneck, we can snapshot the levels slice too.
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, level := range e.levels {
		for _, sst := range level {
			if val, found := sst.Lookup(mvccKey); found {
				decoded, err := e.decodeStoredValue(val, true)
				return decoded, err
			}
		}
	}
	return nil, nil
}

// GetLatestInfo returns the latest value and timestamp for a key.
// OPTIMIZATION: Uses binary search (findGreaterOrEqual) instead of O(N) iterator.
// O(N) → O(log N) for each key.
//
// Returns:
//   - (value, ts, true, nil) — key exists with value
//   - (nil, ts, false, nil) — key was deleted (tombstone at ts)
//   - (nil, 0, false, nil) — key not found
//   - (nil, 0, false, err) — error
//
// The third return value (found) is critical for callers to distinguish
// "key deleted" (tombstone, found=false, ts>0) from "key exists with empty value"
// (found=true, val=nil). See: BUG-001
func (e *LSMEngine) GetLatestInfo(key []byte) ([]byte, uint64, bool, error) {
	if e.closed.Load() {
		return nil, 0, false, fmt.Errorf("engine closed")
	}

	// CRITICAL: Copy MemTable pointers under RLock, then release lock.
	// Holding RLock during SSTable scan starves flushMemTable.
	// A key may live in any shard's active or frozen MemTable, so we scan all.
	// See: SYMPTOM-04, HOT-01
	e.mu.RLock()
	mtList := make([]*memtable.MemTable, 0, len(e.shards)*2)
	for _, shard := range e.shards {
		if shard.memTable != nil {
			mtList = append(mtList, shard.memTable)
		}
		if shard.frozenMemTable != nil {
			mtList = append(mtList, shard.frozenMemTable)
		}
	}
	levels := make([][]*sstable.Reader, len(e.levels))
	for i, level := range e.levels {
		levels[i] = append([]*sstable.Reader(nil), level...)
		for _, sst := range levels[i] {
			sst.Acquire()
		}
	}
	e.mu.RUnlock()

	// Release the reader references acquired above. A concurrent compaction
	// (compactLevel0) may Close() and munmap these readers; Acquire/Release
	// guarantees the mmap region stays valid for the whole scan. See: LSM-02,
	// Глава XIV.
	defer func() {
		for _, level := range levels {
			for _, sst := range level {
				sst.Release()
			}
		}
	}()

	// A key may live in any shard's active or frozen MemTable, so we scan all.
	for _, mt := range mtList {
		val, ts, found := mt.GetLatest(key)
		if ts == 0 {
			continue
		}
		// Key found in memtable (either live or deleted).
		// If found=true, it's a live value. If found=false, it's a tombstone.
		if found {
			decoded, err := e.decodeStoredValue(val, false)
			return decoded, ts, true, err
		}
		// Tombstone — key is deleted, return nil with the tombstone TS.
		return nil, ts, false, nil
	}

	// Search SSTables (already O(log N) via block index).
	for _, level := range levels {
		for _, sst := range level {
			iter, err := sst.NewIterator()
			if err != nil {
				continue
			}
			var bestValue []byte
			var bestTS uint64
			for iter.Next() {
				mvccKey := iter.Key()
				if bytes.Equal(mvccKey.Key, key) {
					commitTS := mvccKey.CommitTS()
					if commitTS > bestTS {
						bestTS = commitTS
						bestValue = iter.Value()
					}
				}
			}
			iter.Close()
			if bestTS > 0 {
				// Tombstones in SSTables have empty value (valLen=0).
				// If the newest version has an empty value, it's a tombstone.
				if len(bestValue) == 0 {
					return nil, bestTS, false, nil
				}
				decoded, err := e.decodeStoredValue(bestValue, true)
				return decoded, bestTS, true, err
			}
		}
	}
	return nil, 0, false, nil
}

// CheckConflict checks if a key has been modified after startTS.
func (e *LSMEngine) CheckConflict(key []byte, startTS uint64) (bool, error) {
	if lastTS, ok := e.getLastCommitCache(key); ok {
		if lastTS > startTS {
			return true, nil
		}
		return false, nil
	}
	_, lastTS, _, err := e.GetLatestInfo(key)
	if err != nil {
		return false, err
	}
	if lastTS > startTS {
		e.updateLastCommitCache(key, lastTS)
		return true, nil
	}
	if lastTS > 0 {
		e.updateLastCommitCache(key, lastTS)
	}
	return false, nil
}

// DeleteWithTS deletes a key with the given commit timestamp.
func (e *LSMEngine) DeleteWithTS(key []byte, commitTS uint64) error {
	// Fast closed check with atomic — no mutex
	if e.closed.Load() {
		return fmt.Errorf("engine closed")
	}
	walEntry := newWalEntry()
	walEntry.Op = OpDelete
	walEntry.Key = key
	walEntry.Value = nil
	walEntry.Timestamp = commitTS
	walEntry.IsLarge = false
	// Route the tombstone to the SAME shard that owns the value. The shard owns
	// both the WAL and the MemTable, so the WAL write and the MemTable Delete
	// hit the same shard, ensuring recovery replays both Put and Delete into the
	// same shard. See: HOT-01, REC-01
	shard := e.shard(key)
	if err := shard.wal.Write(walEntry); err != nil {
		putWalEntry(walEntry)
		return fmt.Errorf("failed to write to wal: %w", err)
	}
	putWalEntry(walEntry)
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	// CRITICAL: Protect memTable access with RLock to prevent race with flushMemTable.
	// See: SYMPTOM-01, SYMPTOM-03, HOT-01
	e.mu.RLock()
	shard.memTable.DeleteWithTS(mvccKey)
	e.mu.RUnlock()
	atomic.AddInt64(&shard.memSize, -int64(len(key)))
	e.updateLastCommitCache(key, commitTS)
	return nil
}

// ActiveMemTable returns the active MemTable of shard 0.
// NOTE: With sharding, there is one active MemTable per shard. This accessor is
// retained for backwards compatibility with callers (e.g. tests) that assume a
// single MemTable; production reads scan every shard via GetWithTS/GetLatestInfo.
// See: HOT-01
func (e *LSMEngine) ActiveMemTable() *memtable.MemTable {
	return e.shards[0].memTable
}

// FrozenMemTable returns the frozen MemTable of shard 0, or nil if none is
// being flushed. Retained for backwards compatibility. See: HOT-01
func (e *LSMEngine) FrozenMemTable() *memtable.MemTable {
	return e.shards[0].frozenMemTable
}

// SetMinActiveSnapshotTS sets the minimum active snapshot timestamp directly.
//
// This is a low-level accessor used primarily by tests. It registers a snapshot
// at the given TS so the reference-counted registry yields it as the minimum.
func (e *LSMEngine) SetMinActiveSnapshotTS(ts uint64) {
	e.snapshotRegistry.RegisterSnapshot(ts)
}

// Shutdown gracefully shuts down the engine with a timeout for VLog view release.
func (e *LSMEngine) Shutdown() error {
	if e.closed.Load() {
		return nil
	}
	e.closed.Store(true)

	// Stop background tasks
	e.stopBackgroundTasks()

	var errs []error

	// Close all shard MemTables (active + frozen)
	e.mu.Lock()
	for _, shard := range e.shards {
		if shard.memTable != nil {
			shard.memTable.Close()
		}
		if shard.frozenMemTable != nil {
			shard.frozenMemTable.Close()
		}
	}
	e.mu.Unlock()

	// Close SSTable readers
	e.mu.RLock()
	for _, level := range e.levels {
		for _, reader := range level {
			if err := reader.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	e.mu.RUnlock()

	// Graceful shutdown for VLog with 5 second timeout
	if e.vlog != nil {
		if err := e.vlog.Shutdown(5 * time.Second); err != nil {
			logger.Warn("VLog shutdown warning: %v", err)
			errs = append(errs, err)
		}
	}

	// Close unified mmap
	if e.unifiedMmap != nil {
		if err := e.unifiedMmap.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// Close all shard WALs. Each shard owns an independent WAL file;
	// closing only e.wal (the shard 0 handle) leaks the rest and prevents their
	// group-commit goroutines from stopping cleanly.
	for _, shard := range e.shards {
		if shard.wal != nil {
			if err := shard.wal.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Close Manifest
	if err := e.manifest.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors while shutting down engine: %v", errs)
	}
	logger.Info("engine shutdown completed gracefully")
	return nil
}

// Close closes the engine and releases all resources.
func (e *LSMEngine) Close() error {
	if e.closed.Load() {
		return nil
	}
	e.closed.Store(true)

	// Stop background tasks
	e.stopBackgroundTasks()
	var errs []error

	// Close all shard MemTables (active + frozen)
	for _, shard := range e.shards {
		if shard.memTable != nil {
			shard.memTable.Close()
		}
		if shard.frozenMemTable != nil {
			shard.frozenMemTable.Close()
		}
	}

	// Close SSTable readers
	for _, level := range e.levels {
		for _, reader := range level {
			if err := reader.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// VLog is closed via Shutdown() or Close() on the VLog itself.
	// DecRef() is NOT called here — it is only used when releasing VLogView
	// instances created via ReadView(). The engine does not hold a View reference.

	// Close unified mmap
	if e.unifiedMmap != nil {
		if err := e.unifiedMmap.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// Close all shard WALs. Each shard owns an independent WAL file;
	// closing only e.wal (the shard 0 handle) leaks the rest and prevents their
	// group-commit goroutines from stopping cleanly.
	for _, shard := range e.shards {
		if shard.wal != nil {
			if err := shard.wal.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}

	// Close Manifest
	if err := e.manifest.Close(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors while closing engine: %v", errs)
	}
	logger.Info("engine closed successfully")
	return nil
}

// copyValue copies src into a fresh heap slice. It is used when a value slice
// references mmap memory that may be unmapped concurrently (extendMmap / Close).
// One allocation — large values are rare and not on the hot path (small values
// are stored inline in the MemTable arena).
func copyValue(src []byte) []byte {
	if len(src) == 0 {
		if src == nil {
			return nil
		}
		return []byte{}
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// decodeStoredValue decodes a stored value (inline, VLog pointer, or unified mmap pointer).
// It reads the leading type tag (v0.4+) to disambiguate a real ValuePointer from
// a user value of exactly 12 bytes.
//
// Checks unified mmap first, then falls back to VLog.
// Uses ReadDirect for zero-copy internal reads.
//
// If fromSSTable is true, the value comes from an SSTable mmap region and must be
// copied before returning, because the mmap region may be released after the reader
// is closed (e.g., during compaction). Values from MemTable are in the arena and
// do not need copying.
//
// See: PROMPT-SSTABLE-FINAL
func (e *LSMEngine) decodeStoredValue(stored []byte, fromSSTable bool) ([]byte, error) {
	if len(stored) == 0 {
		// Empty stored value — return empty slice (not nil) so callers
		// can distinguish "key found with empty value" from "key not found".
		if stored == nil {
			return nil, nil
		}
		return []byte{}, nil
	}

	// Determine the payload and whether it is a ValuePointer, honoring the tag.
	payload := stored
	isPtr := false
	if IsValidValueTag(stored[0]) {
		switch stored[0] {
		case TypeTombstone:
			return nil, nil
		case TypeValuePointer:
			isPtr = true
		default: // TypeInline
		}
		payload = stored[1:]
	} else {
		// Legacy format: no tag — infer from length.
		isPtr = len(stored) == ValuePointerSize
	}

	if isPtr {
		vp, ok := DecodeValuePointer(payload)
		if !ok {
			return payload, nil
		}
		if vp.Offset < 0 || vp.Size <= 0 {
			return payload, nil
		}
		// Check unified mmap first (v0.3.1+ hot path)
		if e.unifiedMmap != nil && vp.Offset < e.unifiedMmap.Size() {
			// ReadValue returns a heap copy made under um.mu, so the returned
			// slice is safe even if extendMmap() remaps the region after the
			// lock is released. No further copy here.
			val, err := e.unifiedMmap.ReadValue(uint64(vp.Offset), vp.Size)
			if err != nil {
				return nil, err
			}
			return val, nil
		}
		// Fall back to VLog (legacy path)
		if vp.Offset+int64(vp.Size)+8 <= e.vlog.Size() {
			val, err := e.vlog.ReadDirect(vp)
			if err != nil {
				return nil, err
			}
			// Same copy for VLog mmap (extendMmap unmaps the old region).
			return copyValue(val), nil
		}
		return payload, nil
	}

	// Inline (or legacy inline) value.
	if fromSSTable {
		val := make([]byte, len(payload))
		copy(val, payload)
		return val, nil
	}
	return payload, nil
}
func (e *LSMEngine) ReadVLogView(vp *ValuePointer) (*VLogView, error) {
	return e.vlog.ReadView(*vp)
}
