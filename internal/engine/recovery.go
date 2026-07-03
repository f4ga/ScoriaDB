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
	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// recoverFromWAL recovers the database state from WAL.
func recoverFromWAL(wal *WAL, memTable *memtable.MemTable, vlog *VLogImpl) error {
	return wal.Recover(func(entry *WalEntry) error {
		switch entry.Op {
		case OpPut:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			if len(entry.Value) == ValuePointerSize {
				if vp, ok := decodeValuePointer(entry.Value); ok {
					if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
						logger.Warn("wal: skipping entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
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
				logger.Warn("wal: failed to decode batch: %v", err)
				return nil
			}
			for _, op := range ops {
				mvccKey := mvcc.NewMVCCKey(op.Key, entry.Timestamp)
				if op.IsDelete {
					memTable.Put(mvccKey, nil)
					continue
				}
				if len(op.Value) == ValuePointerSize {
					if vp, ok := decodeValuePointer(op.Value); ok {
						if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
							logger.Warn("wal: skipping batch entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
								vp.Offset, vp.Size, vlog.Size())
							memTable.Put(mvccKey, nil)
							continue
						}
					} else {
						logger.Warn("wal: skipping batch entry with malformed VLog pointer")
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

// decodeBatchLocal decodes a serialized WriteBatch.
func decodeBatchLocal(data []byte) ([]struct {
	IsDelete bool
	Key      []byte
	Value    []byte
}, error) {
	if len(data) < 2 {
		return nil, errBatchDataTooShort
	}
	pos := 0
	numOps := int(bigEndianUint16(data[pos : pos+2]))
	pos += 2
	ops := make([]struct {
		IsDelete bool
		Key      []byte
		Value    []byte
	}, 0, numOps)
	for i := 0; i < numOps; i++ {
		if pos+1 > len(data) {
			return nil, errMalformedBatchData
		}
		opType := data[pos]
		pos++
		if pos+2 > len(data) {
			return nil, errMalformedBatchData
		}
		keyLen := int(bigEndianUint16(data[pos : pos+2]))
		pos += 2
		if pos+keyLen > len(data) {
			return nil, errMalformedBatchData
		}
		key := make([]byte, keyLen)
		copy(key, data[pos:pos+keyLen])
		pos += keyLen
		if pos+4 > len(data) {
			return nil, errMalformedBatchData
		}
		valLen := int(bigEndianUint32(data[pos : pos+4]))
		pos += 4
		if pos+valLen > len(data) {
			return nil, errMalformedBatchData
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

// Package-level errors for batch decoding.
var (
	errBatchDataTooShort  = &batchError{msg: "batch data too short"}
	errMalformedBatchData = &batchError{msg: "malformed batch data"}
)

type batchError struct{ msg string }

func (e *batchError) Error() string { return e.msg }

// Helper functions to avoid importing encoding/binary.
func bigEndianUint16(b []byte) uint16 {
	return uint16(b[0])<<8 | uint16(b[1])
}

func bigEndianUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
