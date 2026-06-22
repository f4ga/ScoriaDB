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
	memTable            *MemTable
	frozenMemTable      *MemTable
	vlog                *VLogImpl
	wal                 *WAL
	manifest            *Manifest
	vfs                 vfs.VFS
	levels              [][]*sstable.Reader
	LastTS              uint64
	minActiveSnapshotTS uint64
	closed              bool
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

	memTable := NewMemTable()
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
			e.mu.RLock()
			size := e.memSize
			e.mu.RUnlock()

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
func (e *LSMEngine) PutWithTS(key, value []byte, commitTS uint64) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("engine closed")
	}
	var vp ValuePointer
	var inlineValue []byte
	if len(value) <= MaxInlineSize {
		inlineValue = value
	} else {
		var err error
		vp, err = e.vlog.Write(value)
		if err != nil {
			e.mu.Unlock()
			return fmt.Errorf("failed to write to vlog: %w", err)
		}
	}
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	var storedValue []byte
	if vp.Size > 0 {
		storedValue = encodeValuePointer(vp)
	} else {
		storedValue = inlineValue
	}
	walEntry := newWalEntry()
	walEntry.Op = OpPut
	walEntry.Key = key
	walEntry.Value = storedValue
	walEntry.Timestamp = commitTS
	if err := e.wal.Write(walEntry); err != nil {
		putWalEntry(walEntry)
		e.mu.Unlock()
		return fmt.Errorf("failed to write to wal: %w", err)
	}
	putWalEntry(walEntry)
	e.memTable.Put(mvccKey, storedValue)
	atomic.AddInt64(&e.memSize, int64(len(key)+len(value)))
	e.updateLastCommitCache(key, commitTS)
	e.mu.Unlock()
	return nil
}

// WriteAtomicBatch writes an atomic batch of operations.
func (e *LSMEngine) WriteAtomicBatch(data []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	walEntry := newWalEntry()
	walEntry.Op = OpBatch
	walEntry.Key = nil
	walEntry.Value = data
	walEntry.Timestamp = commitTS
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
			e.memTable.Put(mvccKey, nil)
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
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return nil, fmt.Errorf("engine closed")
	}
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)
	val, found := e.memTable.Get(mvccKey)
	if found {
		return e.decodeStoredValue(val)
	}
	for _, level := range e.levels {
		for _, sst := range level {
			if val, found := sst.Lookup(mvccKey); found {
				return e.decodeStoredValue(val)
			}
		}
	}
	return nil, nil
}

// GetLatestInfo returns the latest value and timestamp for a key.
func (e *LSMEngine) GetLatestInfo(key []byte) ([]byte, uint64, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return nil, 0, fmt.Errorf("engine closed")
	}
	searchMemTable := func(mt *MemTable) ([]byte, uint64, bool) {
		if mt == nil {
			return nil, 0, false
		}
		iter := mt.NewIterator()
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
		return bestValue, bestTS, bestValue != nil
	}
	val, ts, found := searchMemTable(e.memTable)
	if found {
		decoded, err := e.decodeStoredValue(val)
		return decoded, ts, err
	}
	val, ts, found = searchMemTable(e.frozenMemTable)
	if found {
		decoded, err := e.decodeStoredValue(val)
		return decoded, ts, err
	}
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
			if bestValue != nil {
				decoded, err := e.decodeStoredValue(bestValue)
				return decoded, bestTS, err
			}
		}
	}
	return nil, 0, nil
}

// CheckConflict checks if a key has been modified after startTS.
func (e *LSMEngine) CheckConflict(key []byte, startTS uint64) (bool, error) {
	if lastTS, ok := e.getLastCommitCache(key); ok {
		if lastTS > startTS {
			return true, nil
		}
		return false, nil
	}
	_, lastTS, err := e.GetLatestInfo(key)
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
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return fmt.Errorf("engine closed")
	}
	walEntry := newWalEntry()
	walEntry.Op = OpDelete
	walEntry.Key = key
	walEntry.Value = nil
	walEntry.Timestamp = commitTS
	if err := e.wal.Write(walEntry); err != nil {
		putWalEntry(walEntry)
		e.mu.Unlock()
		return fmt.Errorf("failed to write to wal: %w", err)
	}
	putWalEntry(walEntry)
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	e.memTable.DeleteWithTS(mvccKey)
	atomic.AddInt64(&e.memSize, -int64(len(key)))
	e.updateLastCommitCache(key, commitTS)
	e.mu.Unlock()
	return nil
}

// ActiveMemTable returns the active MemTable.
func (e *LSMEngine) ActiveMemTable() *MemTable { return e.memTable }

// FrozenMemTable returns the frozen MemTable.
func (e *LSMEngine) FrozenMemTable() *MemTable { return e.frozenMemTable }

// SetMinActiveSnapshotTS sets the minimum active snapshot timestamp.
func (e *LSMEngine) SetMinActiveSnapshotTS(ts uint64) {
	atomic.StoreUint64(&e.minActiveSnapshotTS, ts)
}

// Shutdown gracefully shuts down the engine with a timeout for VLog view release.
func (e *LSMEngine) Shutdown() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

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
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}

	// Stop background tasks
	e.stopBackgroundTasks()

	e.closed = true
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
