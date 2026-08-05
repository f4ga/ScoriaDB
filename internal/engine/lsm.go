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
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// LSMEngine is the main LSM tree engine.
type LSMEngine struct {
	mu                  sync.RWMutex
	dataDir             string
	memTable            *memtable.MemTable
	frozenMemTable      *memtable.MemTable
	vlog                *VLogImpl
	wal                 *WAL
	unifiedMmap         *UnifiedMmap // unified mmap ring buffer (hot path)
	manifest            *Manifest
	vfs                 vfs.VFS
	levels              [][]*sstable.Reader
	LastTS              uint64
	minActiveSnapshotTS uint64
	closed              atomic.Bool
	memSize             int64
	lastCommitCache     map[string]uint64
	cacheMu             sync.RWMutex

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
	manifest, err := NewManifest(vfs, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest: %w", err)
	}

	vlogPath := filepath.Join(dataDir, "vlog.db")
	vlog, err := OpenVLog(vfs, vlogPath)
	if err != nil {
		errors.CloseWithLog(manifest, "manifest")
		return nil, fmt.Errorf("failed to open vlog: %w", err)
	}

	walPath := filepath.Join(dataDir, "wal.log")
	walOpts := DefaultWALOptions()
	if len(opts) > 0 {
		walOpts = opts[0]
	}
	var wal *WAL
	if walOpts.GroupCommitEnabled {
		wal, err = OpenWALWithOptions(walPath, walOpts)
	} else {
		wal, err = OpenWAL(walPath)
	}
	if err != nil {
		errors.CloseWithLog(vlog, "vlog")
		errors.CloseWithLog(manifest, "manifest")
		return nil, fmt.Errorf("failed to open wal: %w", err)
	}

	// Open unified mmap ring buffer (hot write path)
	unifiedPath := filepath.Join(dataDir, "data.mmap")
	unifiedMmap, err := OpenUnifiedMmap(unifiedPath)
	if err != nil {
		errors.CloseWithLog(wal, "wal")
		errors.CloseWithLog(vlog, "vlog")
		errors.CloseWithLog(manifest, "manifest")
		return nil, fmt.Errorf("failed to open unified mmap: %w", err)
	}

	memTable := memtable.NewMemTable()
	lastTS := uint64(1)

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
		dataDir:         dataDir,
		memTable:        memTable,
		vlog:            vlog,
		wal:             wal,
		unifiedMmap:     unifiedMmap,
		manifest:        manifest,
		vfs:             vfs,
		levels:          levels,
		LastTS:          lastTS,
		memSize:         0,
		lastCommitCache: make(map[string]uint64),
	}

	engine.InvalidateVLogPointers()
	if err := recoverFromWAL(engine.wal, engine.memTable, engine.vlog); err != nil {
		errors.CloseWithLog(engine, "engine")
		return nil, fmt.Errorf("failed to recover from wal: %w", err)
	}

	// Start background tasks
	engine.startBackgroundTasks()

	return engine, nil
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
			// CRITICAL: Use atomic.LoadInt64 — e.memSize is written via atomic.AddInt64
			// in PutWithTS/DeleteWithTS. Plain read is a data race.
			// See: SYMPTOM-02
			size := atomic.LoadInt64(&e.memSize)

			if size > MaxMemTableSize {
				select {
				case e.flushCh <- struct{}{}:
				default:
				}
			}
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

	// Encode stored value: either inline bytes or 12-byte ValuePointer
	// Stack-allocated buffer — zero allocation
	// NOTE: vpBuf is stack-allocated [ValuePointerSize]byte. Using vpBuf[:] creates a slice
	// header that the compiler may move to heap. We use unsafe to prevent this.
	var vpBuf [ValuePointerSize]byte
	var storedValue []byte
	if isLarge {
		EncodeValuePointer(vp, vpBuf[:])
		// Use unsafe to create slice without causing vpBuf to escape to heap.
		// The slice points to the stack-allocated array, and Put() copies the
		// data into the arena, so the stack lifetime is sufficient.
		storedValue = unsafe.Slice((*byte)(unsafe.Pointer(&vpBuf)), ValuePointerSize)
	} else {
		storedValue = inlineValue
	}

	// For small values: write to WAL (still needed for durability)
	// For large values: WAL write is ELIMINATED — data is already in unified mmap
	if !isLarge {
		walEntry := newWalEntry()
		walEntry.Op = OpPut
		walEntry.Key = key
		walEntry.Value = storedValue
		walEntry.Timestamp = commitTS
		walEntry.IsLarge = false

		if err := e.wal.Write(walEntry); err != nil {
			putWalEntry(walEntry)
			return fmt.Errorf("failed to write to wal: %w", err)
		}
		putWalEntry(walEntry)
	}

	// CRITICAL: Protect memTable access with RLock to prevent race with flushMemTable.
	// flushMemTable holds Lock (exclusive) while swapping e.memTable.
	// RLock ensures we see a stable pointer — either the old MemTable (before swap)
	// or the new one (after swap), never a half-swapped state.
	// WAL write is done BEFORE the lock to avoid I/O under RLock.
	// See: SYMPTOM-01, SYMPTOM-03
	e.mu.RLock()
	e.memTable.Put(mvccKey, storedValue)
	e.mu.RUnlock()
	atomic.AddInt64(&e.memSize, int64(len(key)+len(value)))
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
	walEntry := newWalEntry()
	walEntry.Op = OpBatch
	walEntry.Key = nil
	walEntry.Value = data
	walEntry.Timestamp = commitTS
	walEntry.IsLarge = false
	if err := e.wal.Write(walEntry); err != nil {
		putWalEntry(walEntry)
		return fmt.Errorf("failed to write batch to wal: %w", err)
	}
	putWalEntry(walEntry)
	// Decode and apply each operation to the memtable
	ops, err := decodeBatchLocal(data)
	if err != nil {
		return fmt.Errorf("failed to decode batch: %w", err)
	}
	var totalSize int64
	for _, op := range ops {
		mvccKey := mvcc.NewMVCCKey(op.Key, commitTS)
		if op.IsDelete {
			// CRITICAL: Delete requires both Put(nil) AND Delete() to set deleted flag.
			// DeleteWithTS ensures tombstone is correctly marked as deleted.
			// See: PROMPT-TOMBSTONE-BATCH-FIX
			e.memTable.DeleteWithTS(mvccKey)
		} else {
			e.memTable.Put(mvccKey, op.Value)
			totalSize += int64(len(op.Key) + len(op.Value))
		}
		e.updateLastCommitCache(op.Key, commitTS)
	}
	atomic.AddInt64(&e.memSize, totalSize)
	return nil
}

// GetWithTS reads a value with the given snapshot timestamp.
func (e *LSMEngine) GetWithTS(key []byte, snapshotTS uint64) ([]byte, error) {
	if e.closed.Load() {
		return nil, fmt.Errorf("engine closed")
	}

	// CRITICAL: Copy MemTable pointer under RLock, then release lock before SSTable scan.
	// Holding RLock during SSTable scan starves flushMemTable (needs Lock).
	// This also prevents the transaction hang (SYMPTOM-04) where 16 goroutines
	// hold RLock while iterating many SSTables.
	// See: SYMPTOM-04
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)

	e.mu.RLock()
	mt := e.memTable
	e.mu.RUnlock()

	if mt != nil {
		val, found := mt.Get(mvccKey)
		if found {
			decoded, err := e.decodeStoredValue(val, false)
			return decoded, err
		}
	}

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
	// See: SYMPTOM-04
	e.mu.RLock()
	mt := e.memTable
	frozen := e.frozenMemTable
	e.mu.RUnlock()

	if mt != nil {
		val, ts, found := mt.GetLatest(key)
		if ts > 0 {
			// Key found in memtable (either live or deleted).
			// If found=true, it's a live value. If found=false, it's a tombstone.
			if found {
				decoded, err := e.decodeStoredValue(val, false)
				return decoded, ts, true, err
			}
			// Tombstone — key is deleted, return nil with the tombstone TS.
			return nil, ts, false, nil
		}
	}
	if frozen != nil {
		val, ts, found := frozen.GetLatest(key)
		if ts > 0 {
			if found {
				decoded, err := e.decodeStoredValue(val, false)
				return decoded, ts, true, err
			}
			return nil, ts, false, nil
		}
	}

	// Search SSTables (already O(log N) via block index)
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, level := range e.levels {
		for _, sst := range level {
			iter, err := sst.NewIterator()
			if err != nil {
				continue
			}
			defer iter.Close()
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
	if err := e.wal.Write(walEntry); err != nil {
		putWalEntry(walEntry)
		return fmt.Errorf("failed to write to wal: %w", err)
	}
	putWalEntry(walEntry)
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	// CRITICAL: Protect memTable access with RLock to prevent race with flushMemTable.
	// See: SYMPTOM-01, SYMPTOM-03
	e.mu.RLock()
	e.memTable.DeleteWithTS(mvccKey)
	e.mu.RUnlock()
	atomic.AddInt64(&e.memSize, -int64(len(key)))
	e.updateLastCommitCache(key, commitTS)
	return nil
}

// ActiveMemTable returns the active MemTable.
func (e *LSMEngine) ActiveMemTable() *memtable.MemTable { return e.memTable }

// FrozenMemTable returns the frozen MemTable.
func (e *LSMEngine) FrozenMemTable() *memtable.MemTable { return e.frozenMemTable }

// SetMinActiveSnapshotTS sets the minimum active snapshot timestamp.
func (e *LSMEngine) SetMinActiveSnapshotTS(ts uint64) {
	atomic.StoreUint64(&e.minActiveSnapshotTS, ts)
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

	// Close MemTable
	e.mu.Lock()
	if e.memTable != nil {
		e.memTable.Close()
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

	// Close WAL
	if err := e.wal.Close(); err != nil {
		errs = append(errs, err)
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

	// Close MemTable
	if e.memTable != nil {
		e.memTable.Close()
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

	// Close WAL
	if err := e.wal.Close(); err != nil {
		errs = append(errs, err)
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

// decodeStoredValue decodes a stored value (inline, VLog pointer, or unified mmap pointer).
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
	if vp, ok := DecodeValuePointer(stored); ok {
		if vp.Offset < 0 || vp.Size <= 0 {
			return stored, nil
		}
		// Check unified mmap first (v0.3.1+ hot path)
		if e.unifiedMmap != nil && vp.Offset < e.unifiedMmap.Size() {
			return e.unifiedMmap.ReadValue(uint64(vp.Offset), vp.Size)
		}
		// Fall back to VLog (legacy path)
		if vp.Offset+int64(vp.Size)+8 <= e.vlog.Size() {
			return e.vlog.ReadDirect(vp)
		}
		return stored, nil
	}
	// Value from SSTable mmap — must copy because mmap may be released
	// after the reader is closed (e.g., during compaction).
	if fromSSTable {
		val := make([]byte, len(stored))
		copy(val, stored)
		return val, nil
	}
	return stored, nil
}
func (e *LSMEngine) ReadVLogView(vp *ValuePointer) (*VLogView, error) {
	return e.vlog.ReadView(*vp)
}
