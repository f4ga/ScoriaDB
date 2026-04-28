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
	"scoriadb/internal/engine"
)

// OpType представляет тип операции в батче.
type OpType int

const (
	OpPut OpType = iota
	OpDelete
)

// BatchOp представляет одну операцию в батче.
type BatchOp struct {
	Type  OpType
	Key   []byte
	Value []byte // для Delete может быть nil
}

// WriteBatch представляет атомарный батч операций.
type WriteBatch struct {
	ops []BatchOp
}

// NewWriteBatch создает новый пустой WriteBatch.
func NewWriteBatch() *WriteBatch {
	return &WriteBatch{
		ops: make([]BatchOp, 0),
	}
}

// AddPut добавляет операцию записи в батч.
func (b *WriteBatch) AddPut(key, value []byte) {
	b.ops = append(b.ops, BatchOp{
		Type:  OpPut,
		Key:   key,
		Value: value,
	})
}

// AddDelete добавляет операцию удаления в батч.
func (b *WriteBatch) AddDelete(key []byte) {
	b.ops = append(b.ops, BatchOp{
		Type:  OpDelete,
		Key:   key,
		Value: nil,
	})
}

// Size возвращает количество операций в батче.
func (b *WriteBatch) Size() int {
	return len(b.ops)
}

// Clear очищает батч.
func (b *WriteBatch) Clear() {
	b.ops = b.ops[:0]
}

// ApplyBatch применяет все операции батча атомарно к движку.
// Возвращает commit timestamp, который был использован для всех операций.
// Если происходит ошибка, ни одна операция не применяется.
func ApplyBatch(db *engine.LSMEngine, batch *WriteBatch) (uint64, error) {
	if batch.Size() == 0 {
		return 0, nil
	}

	// Генерируем единый commit timestamp.
	commitTS := db.NextTimestamp()

	// Сериализуем батч.
	encoded, err := EncodeBatch(batch)
	if err != nil {
		return 0, fmt.Errorf("failed to encode batch: %w", err)
	}

	// Применяем батч атомарно через новый метод.
	if err := db.WriteAtomicBatch(encoded, commitTS); err != nil {
		return 0, fmt.Errorf("failed to apply batch atomically: %w", err)
	}

	return commitTS, nil
}

// ApplyBatchWithTS применяет батч с заданным commit timestamp.
// Используется внутри транзакций, где timestamp уже известен.
func ApplyBatchWithTS(db *engine.LSMEngine, batch *WriteBatch, commitTS uint64) error {
	if batch.Size() == 0 {
		return nil
	}
	// Сериализуем батч.
	encoded, err := EncodeBatch(batch)
	if err != nil {
		return fmt.Errorf("failed to encode batch: %w", err)
	}
	return db.WriteAtomicBatch(encoded, commitTS)
}

// EncodeBatch сериализует WriteBatch в байты для хранения в WAL.
// Формат: количество операций (2 байта) + для каждой операции:
//   тип (1 байт) + длина ключа (2 байта) + ключ + длина значения (4 байта) + значение
func EncodeBatch(batch *WriteBatch) ([]byte, error) {
	// Сначала вычисляем общий размер
	totalSize := 2 // для количества операций
	for _, op := range batch.ops {
		totalSize += 1 + 2 + len(op.Key) + 4 + len(op.Value)
	}

	buf := make([]byte, totalSize)
	pos := 0

	// Количество операций
	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(batch.ops)))
	pos += 2

	// Каждая операция
	for _, op := range batch.ops {
		// Тип операции
		if op.Type == OpPut {
			buf[pos] = 1
		} else if op.Type == OpDelete {
			buf[pos] = 2
		} else {
			return nil, fmt.Errorf("unknown operation type: %v", op.Type)
		}
		pos++

		// Длина ключа
		binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(len(op.Key)))
		pos += 2

		// Ключ
		copy(buf[pos:pos+len(op.Key)], op.Key)
		pos += len(op.Key)

		// Длина значения
		binary.BigEndian.PutUint32(buf[pos:pos+4], uint32(len(op.Value)))
		pos += 4

		// Значение
		copy(buf[pos:pos+len(op.Value)], op.Value)
		pos += len(op.Value)
	}

	return buf, nil
}

// DecodeBatch десериализует WriteBatch из байтов.
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

		if opType == 1 {
			batch.AddPut(key, value)
		} else if opType == 2 {
			batch.AddDelete(key)
		} else {
			return nil, fmt.Errorf("unknown operation type in batch: %d", opType)
		}
	}

	return batch, nil
}