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
	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
	"sync"
	"sync/atomic"
)

type LSMEngine struct {
	mu                  sync.RWMutex
	dataDir             string
	memTable            *MemTable
	frozenMemTable      *MemTable
	vlog                *VLog
	wal                 *WAL
	manifest            *Manifest
	vfs                 vfs.VFS
	levels              [][]*sstable.Reader
	LastTS              uint64
	minActiveSnapshotTS uint64
	closed              bool
	memSize             int64
	lastCommitCache     sync.Map
}

// NewLSMEngine создаёт движок. Если передан параметр WALOptions, используется Group Commit.
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
		manifest.Close()
		return nil, fmt.Errorf("failed to open vlog: %w", err)
	}

	walPath := filepath.Join(dataDir, "wal.log")
	var wal *WAL
	if len(opts) > 0 && opts[0].GroupCommitEnabled {
		wal, err = OpenWALWithOptions(walPath, opts[0])
	} else {
		wal, err = OpenWAL(walPath)
	}
	if err != nil {
		vlog.Close()
		manifest.Close()
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

	engine.InvalidateVLogPointers()
	if err := recoverFromWAL(engine.wal, engine.memTable, engine.vlog); err != nil {
		engine.Close()
		return nil, fmt.Errorf("failed to recover from wal: %w", err)
	}
	return engine, nil
}

func (e *LSMEngine) NextTimestamp() uint64 {
	return atomic.AddUint64(&e.LastTS, 1)
}

func (e *LSMEngine) RegisterSnapshot(snapshotTS uint64) {
	for {
		oldMin := atomic.LoadUint64(&e.minActiveSnapshotTS)
		if oldMin == 0 || snapshotTS < oldMin {
			if atomic.CompareAndSwapUint64(&e.minActiveSnapshotTS, oldMin, snapshotTS) {
				return
			}
			continue
		}
		return
	}
}

func (e *LSMEngine) UnregisterSnapshot(snapshotTS uint64) {
	oldMin := atomic.LoadUint64(&e.minActiveSnapshotTS)
	if oldMin == snapshotTS {
		atomic.StoreUint64(&e.minActiveSnapshotTS, 0)
	}
}

func (e *LSMEngine) GetMinActiveSnapshotTS() uint64 {
	return atomic.LoadUint64(&e.minActiveSnapshotTS)
}

func (e *LSMEngine) updateLastCommitCache(key []byte, commitTS uint64) {
	e.lastCommitCache.Store(string(key), commitTS)
}

func (e *LSMEngine) getLastCommitCache(key []byte) (uint64, bool) {
	val, ok := e.lastCommitCache.Load(string(key))
	if !ok {
		return 0, false
	}
	return val.(uint64), true
}

func (e *LSMEngine) PutWithTS(key, value []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
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
	walEntry := &WalEntry{
		Op:        OpPut,
		Key:       key,
		Value:     storedValue,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}
	e.memTable.Put(mvccKey, storedValue)
	e.updateLastCommitCache(key, commitTS)
	return nil
}

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

func (e *LSMEngine) DeleteWithTS(key []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	walEntry := &WalEntry{
		Op:        OpDelete,
		Key:       key,
		Value:     nil,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	e.memTable.DeleteWithTS(mvccKey)
	e.updateLastCommitCache(key, commitTS)
	return nil
}

func (e *LSMEngine) WriteAtomicBatch(data []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return fmt.Errorf("engine closed")
	}
	if len(data) == 0 {
		return nil
	}
	ops, err := decodeBatchLocal(data)
	if err != nil {
		return fmt.Errorf("failed to decode batch: %w", err)
	}
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
			}{IsDelete: true, Key: op.Key, Value: nil})
		} else {
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
			}{IsDelete: false, Key: op.Key, Value: storedValue})
		}
	}
	tempData, err := encodeBatchLocal(processedOps)
	if err != nil {
		return fmt.Errorf("failed to re-encode batch: %w", err)
	}
	walEntry := &WalEntry{
		Op:        OpBatch,
		Key:       []byte{},
		Value:     tempData,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write batch to wal: %w", err)
	}
	for _, pop := range processedOps {
		mvccKey := mvcc.NewMVCCKey(pop.Key, commitTS)
		if pop.IsDelete {
			e.memTable.DeleteWithTS(mvccKey)
		} else {
			e.memTable.Put(mvccKey, pop.Value)
		}
		e.updateLastCommitCache(pop.Key, commitTS)
	}
	return nil
}

func (e *LSMEngine) ActiveMemTable() *MemTable { return e.memTable }
func (e *LSMEngine) FrozenMemTable() *MemTable { return e.frozenMemTable }

func (e *LSMEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	e.closed = true
	var errs []error
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

func (e *LSMEngine) decodeStoredValue(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	if vp, ok := decodeValuePointer(stored); ok {
		if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > e.vlog.Size() {
			return stored, nil
		}
		return e.vlog.Read(vp)
	}
	return stored, nil
}

func encodeValuePointer(vp ValuePointer) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(vp.Size))
	return buf
}

func decodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != 12 {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:12])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}

func (e *LSMEngine) ReadVLogValue(fileID uint64, offset uint32) ([]byte, error) {
	if e.vlog == nil {
		return nil, fmt.Errorf("vlog not initialized")
	}
	vp := ValuePointer{Offset: int64(fileID), Size: int32(offset)}
	return e.vlog.Read(vp)
}

func (e *LSMEngine) CollectLiveValuePointers() (map[ValuePointer]struct{}, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.closed {
		return nil, fmt.Errorf("engine closed")
	}
	livePointers := make(map[ValuePointer]struct{})
	processValue := func(value []byte) {
		if len(value) == 12 {
			if vp, ok := decodeValuePointer(value); ok {
				livePointers[vp] = struct{}{}
			}
		}
	}
	iter := e.memTable.NewIterator()
	defer iter.Close()
	for iter.Next() {
		processValue(iter.Value())
	}
	if e.frozenMemTable != nil {
		iter := e.frozenMemTable.NewIterator()
		defer iter.Close()
		for iter.Next() {
			processValue(iter.Value())
		}
	}
	for level := 0; level < len(e.levels); level++ {
		for _, reader := range e.levels[level] {
			iter, err := reader.NewIterator()
			if err != nil {
				log.Printf("gc: failed to create iterator for SSTable: %v", err)
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

func (e *LSMEngine) InvalidateVLogPointers() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	processTable := func(mt *MemTable) {
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
	processTable(e.memTable)
	if e.frozenMemTable != nil {
		processTable(e.frozenMemTable)
	}
}

func recoverFromWAL(wal *WAL, memTable *MemTable, vlog *VLog) error {
	return wal.Recover(func(entry *WalEntry) error {
		switch entry.Op {
		case OpPut:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			if len(entry.Value) == 12 {
				if vp, ok := decodeValuePointer(entry.Value); ok {
					if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
						log.Printf("wal: skipping entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
							vp.Offset, vp.Size, vlog.Size())
						return nil
					}
				}
			}
			memTable.Put(mvccKey, entry.Value)
		case OpDelete:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			memTable.Put(mvccKey, nil)
		case OpBatch:
			ops, err := decodeBatchLocal(entry.Value)
			if err != nil {
				log.Printf("wal: failed to decode batch: %v", err)
				return nil
			}
			for _, op := range ops {
				mvccKey := mvcc.NewMVCCKey(op.Key, entry.Timestamp)
				if op.IsDelete {
					memTable.Put(mvccKey, nil)
					continue
				}
				if len(op.Value) == 12 {
					if vp, ok := decodeValuePointer(op.Value); ok {
						if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
							log.Printf("wal: skipping batch entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
								vp.Offset, vp.Size, vlog.Size())
							memTable.Put(mvccKey, nil)
							continue
						}
					} else {
						log.Printf("wal: skipping batch entry with malformed VLog pointer")
						memTable.Put(mvccKey, nil)
						continue
					}
				}
				memTable.Put(mvccKey, op.Value)
			}
		}
		return nil
	})
}

// decodeBatchLocal декодирует сериализованный WriteBatch.
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
			IsDelete: opType == 2,
			Key:      key,
			Value:    value,
		})
	}
	return ops, nil
}

// encodeBatchLocal кодирует список операций в формат WriteBatch.
func encodeBatchLocal(ops []struct {
	IsDelete bool
	Key      []byte
	Value    []byte
}) ([]byte, error) {
	totalSize := 2
	for _, op := range ops {
		totalSize += 1 + 2 + len(op.Key) + 4 + len(op.Value)
	}
	buf := make([]byte, totalSize)
	pos := 0
	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(ops)))
	pos += 2
	for _, op := range ops {
		if op.IsDelete {
			buf[pos] = 2
		} else {
			buf[pos] = 1
		}
		pos++
		binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(op.Key)))
		pos += 2
		copy(buf[pos:pos+len(op.Key)], op.Key)
		pos += len(op.Key)
		binary.BigEndian.PutUint32(buf[pos:pos+4], uint32(len(op.Value)))
		pos += 4
		copy(buf[pos:pos+len(op.Value)], op.Value)
		pos += len(op.Value)
	}
	return buf, nil
}
