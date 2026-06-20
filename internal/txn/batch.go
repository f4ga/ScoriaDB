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

package txn

import (
	"encoding/binary"
	"fmt"
)

// OpType represents operation type in a batch.
type OpType int

const (
	OpPut OpType = iota
	OpDelete
)

// BatchOp represents a single operation in a batch.
type BatchOp struct {
	Type  OpType
	Key   []byte
	Value []byte
}

// WriteBatch represents an atomic batch of operations.
type WriteBatch struct {
	ops []BatchOp
}

// NewWriteBatch creates a new empty WriteBatch.
func NewWriteBatch() *WriteBatch {
	return &WriteBatch{
		ops: make([]BatchOp, 0),
	}
}

// AddPut adds a Put operation to the batch.
func (b *WriteBatch) AddPut(key, value []byte) {
	b.ops = append(b.ops, BatchOp{
		Type:  OpPut,
		Key:   key,
		Value: value,
	})
}

// AddDelete adds a Delete operation to the batch.
func (b *WriteBatch) AddDelete(key []byte) {
	b.ops = append(b.ops, BatchOp{
		Type:  OpDelete,
		Key:   key,
		Value: nil,
	})
}

// Size returns the number of operations in the batch.
func (b *WriteBatch) Size() int {
	return len(b.ops)
}

// Clear clears the batch.
func (b *WriteBatch) Clear() {
	b.ops = b.ops[:0]
}

// ApplyBatch applies all operations atomically to the engine.
// Returns the commit timestamp used.
func ApplyBatch(db interface {
	NextTimestamp() uint64
	WriteAtomicBatch([]byte, uint64) error
}, batch *WriteBatch) (uint64, error) {
	if batch.Size() == 0 {
		return 0, nil
	}

	commitTS := db.NextTimestamp()

	encoded, err := EncodeBatch(batch)
	if err != nil {
		return 0, fmt.Errorf("failed to encode batch: %w", err)
	}

	if err := db.WriteAtomicBatch(encoded, commitTS); err != nil {
		return 0, fmt.Errorf("failed to apply batch atomically: %w", err)
	}

	return commitTS, nil
}

// ApplyBatchWithTS applies the batch with a given commit timestamp.
func ApplyBatchWithTS(db interface {
	WriteAtomicBatch([]byte, uint64) error
}, batch *WriteBatch, commitTS uint64) error {
	if batch.Size() == 0 {
		return nil
	}

	encoded, err := EncodeBatch(batch)
	if err != nil {
		return fmt.Errorf("failed to encode batch: %w", err)
	}

	return db.WriteAtomicBatch(encoded, commitTS)
}

// EncodeBatch serializes WriteBatch into bytes for WAL storage.
func EncodeBatch(batch *WriteBatch) ([]byte, error) {
	totalSize := 2
	for _, op := range batch.ops {
		totalSize += 1 + 2 + len(op.Key) + 4 + len(op.Value)
	}

	buf := make([]byte, totalSize)
	pos := 0

	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(batch.ops)))
	pos += 2

	for _, op := range batch.ops {
		switch op.Type {
		case OpPut:
			buf[pos] = 1
		case OpDelete:
			buf[pos] = 2
		default:
			return nil, fmt.Errorf("unknown operation type: %v", op.Type)
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

// DecodeBatch deserializes WriteBatch from bytes.
func DecodeBatch(data []byte) (*WriteBatch, error) {
	if len(data) < 2 {
		return nil, fmt.Errorf("batch data too short")
	}

	pos := 0
	numOps := binary.BigEndian.Uint16(data[pos : pos+2])
	pos += 2

	batch := NewWriteBatch()

	for i := 0; i < int(numOps); i++ {
		if pos+1 > len(data) {
			return nil, fmt.Errorf("malformed batch data: missing op type")
		}
		opType := data[pos]
		pos++

		if pos+2 > len(data) {
			return nil, fmt.Errorf("malformed batch data: missing key length")
		}
		keyLen := binary.BigEndian.Uint16(data[pos : pos+2])
		pos += 2

		if pos+int(keyLen) > len(data) {
			return nil, fmt.Errorf("malformed batch data: key length exceeds buffer")
		}
		key := make([]byte, keyLen)
		copy(key, data[pos:pos+int(keyLen)])
		pos += int(keyLen)

		if pos+4 > len(data) {
			return nil, fmt.Errorf("malformed batch data: missing value length")
		}
		valLen := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 4

		if pos+int(valLen) > len(data) {
			return nil, fmt.Errorf("malformed batch data: value length exceeds buffer")
		}
		value := make([]byte, valLen)
		copy(value, data[pos:pos+int(valLen)])
		pos += int(valLen)

		switch opType {
		case 1:
			batch.AddPut(key, value)
		case 2:
			batch.AddDelete(key)
		default:
			return nil, fmt.Errorf("unknown operation type in batch: %d", opType)
		}
	}

	return batch, nil
}
