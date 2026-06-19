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
	"encoding/binary"
	"fmt"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// decodeBatchLocal decodes a serialized WriteBatch.
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

// encodeBatchLocal encodes a list of operations into WriteBatch format.
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

// WriteAtomicBatch writes an atomic batch of operations.
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
