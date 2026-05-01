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
	"encoding/binary"
	"fmt"
	"log"
	"path/filepath"
	"scoriadb/internal/engine/sstable"
	"scoriadb/internal/engine/vfs"
	"scoriadb/internal/mvcc"
	"sync"
	"sync/atomic"
)

// LSMEngine represents an LSM engine with VLog and MVCC.
type LSMEngine struct {
	mu                  sync.RWMutex
	dataDir             string
	memTable            *MemTable
	frozenMemTable      *MemTable
	vlog                *VLog
	wal                 *WAL
	manifest            *Manifest           // SSTable metadata journal
	vfs                 vfs.VFS             // filesystem abstraction
	levels              [][]*sstable.Reader // SSTable levels (Level0, Level1, ...)
	LastTS              uint64              // atomic timestamp counter
	minActiveSnapshotTS uint64              // atomic, minimum timestamp of active snapshots
	closed              bool
	memSize             int64 // approximate MemTable size in bytes
	// lastCommitCache stores the latest commitTS for each user key (fast conflict detection)
	lastCommitCache sync.Map
}

// NewLSMEngine creates a new LSM engine.
func NewLSMEngine(dataDir string) (*LSMEngine, error) {
	// Create VFS (standard implementation using os)
	vfs := vfs.NewDefaultVFS()

	// Create directory via VFS
	if err := vfs.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Open manifest
	manifestPath := filepath.Join(dataDir, "MANIFEST")
	manifest, err := NewManifest(vfs, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest: %w", err)
	}

	// Open VLog
	vlogPath := filepath.Join(dataDir, "vlog.db")
	vlog, err := OpenVLog(vfs, vlogPath)
	if err != nil {
		manifest.Close()
		return nil, fmt.Errorf("failed to open vlog: %w", err)
	}

	// Open WAL (currently using old API)
	walPath := filepath.Join(dataDir, "wal.log")
	wal, err := OpenWAL(walPath)
	if err != nil {
		vlog.Close()
		manifest.Close()
		return nil, fmt.Errorf("failed to open wal: %w", err)
	}

	// Create empty MemTable
	memTable := NewMemTable()

	// Determine last timestamp (simple counter for now)
	lastTS := uint64(1) // TODO: restore from persisted data

	// Load existing SSTables from manifest
	levels := make([][]*sstable.Reader, 10) // assume at most 10 levels
	manifestLevels := manifest.GetLevels()
	for level, infos := range manifestLevels {
		if level >= len(levels) {
			continue
		}
		for _, info := range infos {
			// Build SSTable file path
			sstPath := filepath.Join(dataDir, fmt.Sprintf("%06d.sst", info.FileNum))
			reader, err := sstable.Open(sstPath)
			if err != nil {
				// If file missing, ignore (maybe deleted)
				continue
			}
			levels[level] = append(levels[level], reader)
		}
	}

	engine := &LSMEngine{
		dataDir:  dataDir,
		memTable: memTable,
		vlog:     vlog,
		wal:      wal,
		manifest: manifest,
		vfs:      vfs,
		levels:   levels,
		LastTS:   lastTS,
		memSize:  0,
	}
	// After VLog may have been recreated (magic mismatch), invalidate any remaining pointers
	// BEFORE recovering from WAL, to avoid conflicts with stale pointers
	engine.InvalidateVLogPointers()
	// Recover data from WAL
	if err := recoverFromWAL(engine.wal, engine.memTable, engine.vlog); err != nil {
		engine.Close()
		return nil, fmt.Errorf("failed to recover from wal: %w", err)
	}
	return engine, nil
}

// NextTimestamp returns the next unique timestamp (atomically increments LastTS).
func (e *LSMEngine) NextTimestamp() uint64 {
	// Use atomic operation to increment LastTS
	return atomic.AddUint64(&e.LastTS, 1)
}

// RegisterSnapshot registers an active snapshot with the given timestamp.
// Updates minActiveSnapshotTS if this snapshot is the oldest active one.
func (e *LSMEngine) RegisterSnapshot(snapshotTS uint64) {
	for {
		oldMin := atomic.LoadUint64(&e.minActiveSnapshotTS)
		if oldMin == 0 || snapshotTS < oldMin {
			if atomic.CompareAndSwapUint64(&e.minActiveSnapshotTS, oldMin, snapshotTS) {
				return
			}
			// CAS failed, retry
			continue
		}
		return
	}
}

// UnregisterSnapshot removes an active snapshot.
// If this snapshot was the minimum, we need to find the new minimum.
// For simplicity, we reset to 0 and rely on other snapshots to update it.
// In a production system, we would maintain a data structure of all active snapshots.
func (e *LSMEngine) UnregisterSnapshot(snapshotTS uint64) {
	oldMin := atomic.LoadUint64(&e.minActiveSnapshotTS)
	if oldMin == snapshotTS {
		// The snapshot being removed is the current minimum.
		// We don't know the next minimum, so reset to 0.
		// The next RegisterSnapshot will set it correctly.
		// This is safe because compaction checks for 0 (no active snapshots).
		atomic.StoreUint64(&e.minActiveSnapshotTS, 0)
	}
}

// GetMinActiveSnapshotTS returns the minimum timestamp of active snapshots.
// Returns 0 if there are no active snapshots.
func (e *LSMEngine) GetMinActiveSnapshotTS() uint64 {
	return atomic.LoadUint64(&e.minActiveSnapshotTS)
}

// updateLastCommitCache atomically stores the latest commitTS for a key.
func (e *LSMEngine) updateLastCommitCache(key []byte, commitTS uint64) {
	e.lastCommitCache.Store(string(key), commitTS)
}

// getLastCommitCache returns the latest known commitTS for a key from cache.
func (e *LSMEngine) getLastCommitCache(key []byte) (uint64, bool) {
	val, ok := e.lastCommitCache.Load(string(key))
	if !ok {
		return 0, false
	}
	return val.(uint64), true
}

// PutWithTS writes a key‑value pair with the given timestamp.
func (e *LSMEngine) PutWithTS(key, value []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	// Decide whether to write to VLog
	var vp ValuePointer
	var inlineValue []byte
	if len(value) <= MaxInlineSize {
		inlineValue = value
	} else {
		var err error
		vp, err = e.vlog.Write(value)
		if err != nil {
			return fmt.Errorf("failed to write to vlog: %w", err)
		}
	}

	// Create MVCCKey
	mvccKey := mvcc.NewMVCCKey(key, commitTS)

	// Prepare value for MemTable
	var storedValue []byte
	if vp.Size > 0 {
		// Serialize ValuePointer
		storedValue = encodeValuePointer(vp)
	} else {
		storedValue = inlineValue
	}

	// Write to WAL
	walEntry := &WalEntry{
		Op:        OpPut,
		Key:       key,
		Value:     storedValue, // either inline value or pointer
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}

	// Update MemTable
	e.memTable.Put(mvccKey, storedValue)

	// Update last commit cache
	e.updateLastCommitCache(key, commitTS)

	// TODO: check if flush is needed
	return nil
}

// GetWithTS returns the value for a key as of snapshotTS.
func (e *LSMEngine) GetWithTS(key []byte, snapshotTS uint64) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, fmt.Errorf("engine closed")
	}

	// Search in MemTable
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)
	val, found := e.memTable.Get(mvccKey)
	if found {
		return e.decodeStoredValue(val)
	}

	// Search in SSTables (by levels)
	for _, level := range e.levels {
		for _, sst := range level {
			if val, found := sst.Lookup(mvccKey); found {
				return e.decodeStoredValue(val)
			}
		}
	}

	return nil, nil // key not found
}

// GetLatestInfo returns the latest value and its commitTS for a key.
// If the key does not exist, returns (nil, 0, nil).
func (e *LSMEngine) GetLatestInfo(key []byte) ([]byte, uint64, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, 0, fmt.Errorf("engine closed")
	}

	// Helper to search a MemTable and return the latest version with key and value
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

	// Search active MemTable
	val, ts, found := searchMemTable(e.memTable)
	if found {
		decoded, err := e.decodeStoredValue(val)
		return decoded, ts, err
	}

	// Search frozen MemTable
	val, ts, found = searchMemTable(e.frozenMemTable)
	if found {
		decoded, err := e.decodeStoredValue(val)
		return decoded, ts, err
	}

	// Search SSTables
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

	return nil, 0, nil // not found
}

// CheckConflict returns true if the key has been modified after startTS.
// Uses fast cache first, then falls back to full GetLatestInfo.
func (e *LSMEngine) CheckConflict(key []byte, startTS uint64) (bool, error) {
	// Fast path: cache
	if lastTS, ok := e.getLastCommitCache(key); ok {
		if lastTS > startTS {
			return true, nil
		}
		// Cache indicates no conflict. We trust it because we update cache on every write.
		return false, nil
	}

	// Slow path: full lookup
	_, lastTS, err := e.GetLatestInfo(key)
	if err != nil {
		return false, err
	}
	if lastTS > startTS {
		// Also update cache for future
		e.updateLastCommitCache(key, lastTS)
		return true, nil
	}
	if lastTS > 0 {
		e.updateLastCommitCache(key, lastTS)
	}
	return false, nil
}

// DeleteWithTS marks a key as deleted (tombstone).
func (e *LSMEngine) DeleteWithTS(key []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	// Write to WAL
	walEntry := &WalEntry{
		Op:        OpDelete,
		Key:       key,
		Value:     nil,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}

	// Insert tombstone (nil value) into MemTable
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	e.memTable.DeleteWithTS(mvccKey)

	// Update last commit cache (tombstone also updates the latest commitTS)
	e.updateLastCommitCache(key, commitTS)

	return nil
}

// WriteAtomicBatch applies a serialized WriteBatch atomically with a single WAL entry.
// The data parameter must be the result of txn.EncodeBatch(batch).
func (e *LSMEngine) WriteAtomicBatch(data []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	if len(data) == 0 {
		return nil
	}

	// Decode batch operations locally (without importing txn)
	ops, err := decodeBatchLocal(data)
	if err != nil {
		return fmt.Errorf("failed to decode batch: %w", err)
	}

	// Process each operation to handle VLog for large values
	processedOps := make([]struct {
		IsDelete bool
		Key      []byte
		Value    []byte
	}, 0, len(ops))

	for _, op := range ops {
		var storedValue []byte
		if op.IsDelete {
			processedOps = append(processedOps, struct {
				IsDelete bool
				Key      []byte
				Value    []byte
			}{
				IsDelete: true,
				Key:      op.Key,
				Value:    nil,
			})
		} else {
			// Handle VLog for large values (same logic as PutWithTS)
			if len(op.Value) <= MaxInlineSize {
				storedValue = op.Value
			} else {
				vp, err := e.vlog.Write(op.Value)
				if err != nil {
					return fmt.Errorf("failed to write to vlog: %w", err)
				}
				storedValue = encodeValuePointer(vp)
			}
			processedOps = append(processedOps, struct {
				IsDelete bool
				Key      []byte
				Value    []byte
			}{
				IsDelete: false,
				Key:      op.Key,
				Value:    storedValue,
			})
		}
	}

	// Create a temporary batch with processed values for serialization
	// We need to re-encode with processed values to store in WAL
	tempData, err := encodeBatchLocal(processedOps)
	if err != nil {
		return fmt.Errorf("failed to re-encode batch: %w", err)
	}

	// Write batch as single WAL entry
	walEntry := &WalEntry{
		Op:        OpBatch,
		Key:       []byte{}, // empty key for batch
		Value:     tempData,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write batch to wal: %w", err)
	}

	// Apply operations to MemTable and update cache
	for _, pop := range processedOps {
		mvccKey := mvcc.NewMVCCKey(pop.Key, commitTS)
		if pop.IsDelete {
			e.memTable.DeleteWithTS(mvccKey)
		} else {
			e.memTable.Put(mvccKey, pop.Value)
		}
		// Update cache for each key in batch
		e.updateLastCommitCache(pop.Key, commitTS)
	}

	return nil
}

// decodeBatchLocal decodes a serialized batch without importing txn package.
// Format matches txn.EncodeBatch: 2 bytes numOps, then for each operation:
//
//	1 byte type (1=Put, 2=Delete), 2 bytes keyLen, key, 4 bytes valLen, value.
func decodeBatchLocal(data []byte) ([]struct {
	IsDelete bool
	Key      []byte
	Value    []byte
}, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("batch data too short")
	}

	pos := 0
	numOps := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2

	ops := make([]struct {
		IsDelete bool
		Key      []byte
		Value    []byte
	}, 0, numOps)

	for i := 0; i < numOps; i++ {
		if pos+1 > len(data) {
			return nil, fmt.Errorf("malformed batch data: missing op type")
		}
		opType := data[pos]
		pos++

		if pos+2 > len(data) {
			return nil, fmt.Errorf("malformed batch data: missing key length")
		}
		keyLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		pos += 2

		if pos+keyLen > len(data) {
			return nil, fmt.Errorf("malformed batch data: key length exceeds buffer")
		}
		key := make([]byte, keyLen)
		copy(key, data[pos:pos+keyLen])
		pos += keyLen

		if pos+4 > len(data) {
			return nil, fmt.Errorf("malformed batch data: missing value length")
		}
		valLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		pos += 4

		if pos+valLen > len(data) {
			return nil, fmt.Errorf("malformed batch data: value length exceeds buffer")
		}
		value := make([]byte, valLen)
		copy(value, data[pos:pos+valLen])
		pos += valLen

		ops = append(ops, struct {
			IsDelete bool
			Key      []byte
			Value    []byte
		}{
			IsDelete: opType == 2, // 1 for Put, 2 for Delete
			Key:      key,
			Value:    value,
		})
	}

	return ops, nil
}

// encodeBatchLocal encodes processed operations into the batch format.
func encodeBatchLocal(ops []struct {
	IsDelete bool
	Key      []byte
	Value    []byte
}) ([]byte, error) {
	// Calculate total size
	totalSize := 2 // for numOps
	for _, op := range ops {
		totalSize += 1 + 2 + len(op.Key) + 4 + len(op.Value)
	}

	buf := make([]byte, totalSize)
	pos := 0

	// Number of operations
	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(ops)))
	pos += 2

	// Each operation
	for _, op := range ops {
		// Operation type
		if op.IsDelete {
			buf[pos] = 2
		} else {
			buf[pos] = 1
		}
		pos++

		// Key length
		binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(op.Key)))
		pos += 2

		// Key
		copy(buf[pos:pos+len(op.Key)], op.Key)
		pos += len(op.Key)

		// Value length
		binary.BigEndian.PutUint32(buf[pos:pos+4], uint32(len(op.Value)))
		pos += 4

		// Value
		copy(buf[pos:pos+len(op.Value)], op.Value)
		pos += len(op.Value)
	}

	return buf, nil
}

// ActiveMemTable returns the active (current) MemTable.
func (e *LSMEngine) ActiveMemTable() *MemTable {
	return e.memTable
}

// FrozenMemTable returns the frozen MemTable (if any).
func (e *LSMEngine) FrozenMemTable() *MemTable {
	return e.frozenMemTable
}

// Close releases engine resources.
func (e *LSMEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	var errs []error
	// Close SSTable readers
	for _, level := range e.levels {
		for _, reader := range level {
			if err := reader.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := e.vlog.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := e.wal.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := e.manifest.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors while closing engine: %v", errs)
	}
	return nil
}

// decodeStoredValue converts stored value (inline or ValuePointer) back to original value.
func (e *LSMEngine) decodeStoredValue(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		// tombstone
		return nil, nil
	}
	// Try to decode as ValuePointer
	if vp, ok := decodeValuePointer(stored); ok {
		// Validate pointer before reading VLog to avoid out-of-range panic
		if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > e.vlog.Size() {
			// Invalid pointer – treat as inline value (could be ordinary 12-byte data)
			return stored, nil
		}
		// Read from VLog
		return e.vlog.Read(vp)
	}
	// Otherwise it's an inline value
	return stored, nil
}

// encodeValuePointer serializes a ValuePointer to bytes.
func encodeValuePointer(vp ValuePointer) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(vp.Size))
	return buf
}

// decodeValuePointer deserializes a ValuePointer from bytes.
func decodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != 12 {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:12])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}

// ReadVLogValue reads a value from the Value Log by pointer.
// Pointer format: [fileID: 8 bytes][offset: 4 bytes].
// In the current implementation fileID is ignored (always 0) and offset is interpreted as size.
func (e *LSMEngine) ReadVLogValue(fileID uint64, offset uint32) ([]byte, error) {
	if e.vlog == nil {
		return nil, fmt.Errorf("vlog not initialized")
	}
	// Create ValuePointer: fileID becomes offset, offset becomes size
	vp := ValuePointer{Offset: int64(fileID), Size: int32(offset)}
	return e.vlog.Read(vp)
}

// CollectLiveValuePointers returns a set of all ValuePointers that are currently
// referenced by live keys in the LSM tree (MemTable + SSTables).
// This is used for garbage collection of the Value Log.
func (e *LSMEngine) CollectLiveValuePointers() (map[ValuePointer]struct{}, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, fmt.Errorf("engine closed")
	}

	livePointers := make(map[ValuePointer]struct{})

	// Helper function to process a value
	processValue := func(value []byte) {
		if len(value) == 12 {
			// Check if it's a ValuePointer (12 bytes)
			if vp, ok := decodeValuePointer(value); ok {
				livePointers[vp] = struct{}{}
			}
		}
	}

	// 1. Collect from MemTable
	iter := e.memTable.NewIterator()
	defer iter.Close()
	for iter.Next() {
		processValue(iter.Value())
	}

	// 2. Collect from frozen MemTable (if exists)
	if e.frozenMemTable != nil {
		iter := e.frozenMemTable.NewIterator()
		defer iter.Close()
		for iter.Next() {
			processValue(iter.Value())
		}
	}

	// 3. Collect from SSTables
	for level := 0; level < len(e.levels); level++ {
		for _, reader := range e.levels[level] {
			iter, err := reader.NewIterator()
			if err != nil {
				// Log error but continue with other SSTables
				log.Printf("gc: failed to create iterator for SSTable: %v", err)
				continue
			}
			// Defer is tricky in loop; we call close manually after usage
			for iter.Next() {
				processValue(iter.Value())
			}
			iter.Close()
		}
	}

	return livePointers, nil
}

// InvalidateVLogPointers removes all entries in MemTable (active and frozen) that contain
// VLog pointers (12‑byte values). This is called after VLog is recreated (magic mismatch)
// to avoid "value pointer out of range" errors when reading those entries.
func (e *LSMEngine) InvalidateVLogPointers() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return
	}

	// Helper to process a MemTable
	processTable := func(mt *MemTable) {
		iter := mt.NewIterator()
		defer iter.Close()

		var toDelete []mvcc.MVCCKey
		for iter.Next() {
			val := iter.Value()
			if len(val) == 12 {
				// Could be a VLog pointer; we don't need to decode, just delete
				toDelete = append(toDelete, iter.Key())
			}
		}
		// Delete collected entries
		for _, key := range toDelete {
			mt.DeleteWithTS(key)
		}
	}

	processTable(e.memTable)
	if e.frozenMemTable != nil {
		processTable(e.frozenMemTable)
	}
}

// recoverFromWAL recovers MemTable from WAL.
func recoverFromWAL(wal *WAL, memTable *MemTable, vlog *VLog) error {
	return wal.Recover(func(entry *WalEntry) error {
		switch entry.Op {
		case OpPut:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			// Проверяем, является ли значение VLog-указателем (12 байт)
			if len(entry.Value) == 12 {
				if vp, ok := decodeValuePointer(entry.Value); ok {
					// Проверяем, что указатель не выходит за пределы текущего VLog
					// offset + size + 8 (заголовок) должен быть <= размеру VLog
					if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
						log.Printf("wal: skipping entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
							vp.Offset, vp.Size, vlog.Size())
						return nil // пропускаем эту запись
					}
				}
			}
			memTable.Put(mvccKey, entry.Value)
		case OpDelete:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			memTable.Put(mvccKey, nil)
		case OpBatch:
			// Десериализуем батч и применяем его операции
			ops, err := decodeBatchLocal(entry.Value)
			if err != nil {
				log.Printf("wal: failed to decode batch: %v", err)
				return nil // пропускаем повреждённый батч
			}
			// Применяем операции батча
			for _, op := range ops {
				mvccKey := mvcc.NewMVCCKey(op.Key, entry.Timestamp)
				if op.IsDelete {
					memTable.Put(mvccKey, nil)
					continue
				}
				// Для батча значения уже обработаны (VLog указатели или inline)
				// op.Value уже содержит либо inline значение, либо VLog указатель
				if len(op.Value) == 12 {
					if vp, ok := decodeValuePointer(op.Value); ok {
						// Проверяем, что указатель не выходит за пределы текущего VLog
						if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
							log.Printf("wal: skipping batch entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
								vp.Offset, vp.Size, vlog.Size())
							// Сохраняем ключ с tombstone, чтобы не терять факт существования ключа
							memTable.Put(mvccKey, nil)
							continue
						}
					} else {
						// Не удалось декодировать указатель (коррупция) – пропускаем операцию
						log.Printf("wal: skipping batch entry with malformed VLog pointer")
						memTable.Put(mvccKey, nil)
						continue
					}
				}
				// Валидный указатель или inline значение
				memTable.Put(mvccKey, op.Value)
			}
		}
		return nil
	})
}
