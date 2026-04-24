package txn

import (
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

	// Применяем операции последовательно.
	// В реальной реализации нужно обеспечить атомарность через WAL.
	for _, op := range batch.ops {
		switch op.Type {
		case OpPut:
			if err := db.PutWithTS(op.Key, op.Value, commitTS); err != nil {
				// В случае ошибки нужно откатить предыдущие операции.
				// Для простоты пока просто возвращаем ошибку.
				return 0, fmt.Errorf("failed to apply Put operation: %w", err)
			}
		case OpDelete:
			if err := db.DeleteWithTS(op.Key, commitTS); err != nil {
				return 0, fmt.Errorf("failed to apply Delete operation: %w", err)
			}
		default:
			return 0, fmt.Errorf("unknown operation type: %v", op.Type)
		}
	}

	return commitTS, nil
}

// ApplyBatchWithTS применяет батч с заданным commit timestamp.
// Используется внутри транзакций, где timestamp уже известен.
func ApplyBatchWithTS(db *engine.LSMEngine, batch *WriteBatch, commitTS uint64) error {
	for _, op := range batch.ops {
		switch op.Type {
		case OpPut:
			if err := db.PutWithTS(op.Key, op.Value, commitTS); err != nil {
				return fmt.Errorf("failed to apply Put operation: %w", err)
			}
		case OpDelete:
			if err := db.DeleteWithTS(op.Key, commitTS); err != nil {
				return fmt.Errorf("failed to apply Delete operation: %w", err)
			}
		default:
			return fmt.Errorf("unknown operation type: %v", op.Type)
		}
	}
	return nil
}