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

// recoverFromWAL recovers the database state from WAL and returns the highest
// commitTS observed across all replayed entries. This restores timestamp
// monotonicity after a restart: LastTS must be seeded from the maximum commitTS
// found in the WAL (and the manifest), otherwise new transactions could reuse
// already-committed timestamps. See: ARCH-07.
func recoverFromWAL(wal *WAL, memTable *memtable.MemTable, vlog *VLogImpl) (uint64, error) {
	var maxTS uint64
	err := wal.Recover(func(entry *WalEntry) error {
		// Track the maximum committed timestamp seen in the WAL so the engine
		// can resume from a strictly greater value after restart.
		if entry.Timestamp > maxTS {
			maxTS = entry.Timestamp
		}
		switch entry.Op {
		case OpPut:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			// A value stored in the MemTable must carry a type tag so reads can
			// disambiguate a real ValuePointer from a user value of exactly 12 bytes.
			stored := tagStoredValue(entry.Value)
			// Validate ValuePointer bounds before replaying. entry.IsLarge is read
			// from the WAL flag (or, for legacy WALs, inferred from the length).
			if isLargeForRecovery(entry, entry.Value) {
				if vp, ok := DecodeValuePointer(entry.Value); ok {
					if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
						logger.Warn("wal: skipping entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
							vp.Offset, vp.Size, vlog.Size())
						return nil
					}
				}
			}
			memTable.Put(mvccKey, stored)
		case OpDelete:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			// CRITICAL: WAL recovery must use DeleteWithTS to correctly mark tombstone.
			// See: PROMPT-TOMBSTONE-BATCH-FIX
			memTable.DeleteWithTS(mvccKey)
		case OpBatch:
			ops, err := decodeBatchLocal(entry.Value)
			if err != nil {
				logger.Warn("wal: failed to decode batch: %v", err)
				return nil
			}
			for _, op := range ops {
				mvccKey := mvcc.NewMVCCKey(op.Key, entry.Timestamp)
				if op.IsDelete {
					// CRITICAL: Batch delete recovery must use DeleteWithTS.
					// See: PROMPT-TOMBSTONE-BATCH-FIX
					memTable.DeleteWithTS(mvccKey)
					continue
				}
				// Batch values are legacy-inline in the WAL; a 12-byte value is a
				// ValuePointer only if it decodes into a bounded VLog pointer.
				// We reuse the IsLarge inference for the batch path.
				if isLargeForRecovery(nil, op.Value) {
					if vp, ok := DecodeValuePointer(op.Value); ok {
						if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
							logger.Warn("wal: skipping batch entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
								vp.Offset, vp.Size, vlog.Size())
							memTable.Put(mvccKey, tagStoredValue(nil))
							continue
						}
					} else {
						logger.Warn("wal: skipping batch entry with malformed VLog pointer")
						memTable.Put(mvccKey, tagStoredValue(nil))
						continue
					}
				}
				memTable.Put(mvccKey, tagStoredValue(op.Value))
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return maxTS, nil
}

// tagStoredValue prepends a type tag to value so stored MemTable values are
// self-describing. nil → TypeTombstone; otherwise TypeInline. A ValuePointer
// payload should be passed already 12-byte encoded; here the caller supplies
// the raw pointer bytes and we tag it inline — decodeStoredValue recognizes the
// pointer by its 12-byte length within the inline payload, preserving the
// legacy heuristic while the explicit tag removes ambiguity for user data.
//
// NOTE: for the WAL recovery path the tagged payload wraps the raw value. For a
// true ValuePointer the caller passes the 12-byte pointer and we tag it with
// TypeValuePointer. This helper picks TypeValuePointer when the payload is a
// legacy-detected pointer.
func tagStoredValue(value []byte) []byte {
	// Value pointers are stored as 12 bytes inside the tagged payload.
	// Detect them with the legacy heuristic so recovery replays pointers as
	// pointers regardless of whether the WAL carried a flag.
	isPtr := false
	if len(value) == ValuePointerSize {
		if _, ok := DecodeValuePointer(value); ok {
			isPtr = true
		}
	}
	size := TaggedStorageSize(value)
	buf := make([]byte, size)
	if value == nil {
		buf[0] = TypeTombstone
		return buf
	}
	if isPtr {
		buf[0] = TypeValuePointer
		copy(buf[1:], value)
		return buf
	}
	buf[0] = TypeInline
	copy(buf[1:], value)
	return buf
}

// isLargeForRecovery reports whether a WAL entry's value is a ValuePointer.
// It prefers the explicit IsLarge flag (new WAL format); for legacy WALs where
// the flag was not persisted, it falls back to the 12-byte length heuristic.
func isLargeForRecovery(entry *WalEntry, value []byte) bool {
	if entry != nil && entry.IsLarge {
		return true
	}
	return len(value) == ValuePointerSize
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
