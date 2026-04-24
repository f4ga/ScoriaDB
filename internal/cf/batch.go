package cf

import (
	"fmt"
	"scoriadb/internal/engine"
	"scoriadb/internal/txn"
)

// CFBatchOp представляет операцию в батче с указанием Column Family.
type CFBatchOp struct {
	CF    string
	Type  txn.OpType
	Key   []byte
	Value []byte // для Delete может быть nil
}

// CFWriteBatch представляет атомарный батч операций, которые могут затрагивать несколько CF.
type CFWriteBatch struct {
	ops []CFBatchOp
}

// NewCFWriteBatch создает новый пустой CFWriteBatch.
func NewCFWriteBatch() *CFWriteBatch {
	return &CFWriteBatch{
		ops: make([]CFBatchOp, 0),
	}
}

// AddPut добавляет операцию записи в указанное CF.
func (b *CFWriteBatch) AddPut(cf string, key, value []byte) {
	b.ops = append(b.ops, CFBatchOp{
		CF:    cf,
		Type:  txn.OpPut,
		Key:   key,
		Value: value,
	})
}

// AddDelete добавляет операцию удаления в указанное CF.
func (b *CFWriteBatch) AddDelete(cf string, key []byte) {
	b.ops = append(b.ops, CFBatchOp{
		CF:    cf,
		Type:  txn.OpDelete,
		Key:   key,
		Value: nil,
	})
}

// Size возвращает количество операций в батче.
func (b *CFWriteBatch) Size() int {
	return len(b.ops)
}

// Clear очищает батч.
func (b *CFWriteBatch) Clear() {
	b.ops = b.ops[:0]
}

// ApplyCFBatch применяет все операции батча атомарно к реестру CF.
// Возвращает commit timestamp, который был использован для всех операций.
// Если происходит ошибка, ни одна операция не применяется.
// Алгоритм:
// 1. Группируем операции по CF.
// 2. Для каждого CF получаем движок через реестр.
// 3. Для каждого движка создаём отдельный WriteBatch (без CF) и применяем его.
// 4. Важно обеспечить атомарность на уровне всех CF: если хотя бы одна операция
//    не может быть выполнена, весь батч откатывается.
// Для простоты MVP применяем операции последовательно и откатываем при ошибке
// путём отмены предыдущих операций (что сложно). Вместо этого можно использовать
// двухфазный коммит с WAL, но пока просто возвращаем ошибку после частичного применения.
// TODO: улучшить атомарность.
func ApplyCFBatch(reg *Registry, batch *CFWriteBatch) (uint64, error) {
	if batch.Size() == 0 {
		return 0, nil
	}

	// Группируем операции по CF
	cfOps := make(map[string][]txn.BatchOp)
	for _, op := range batch.ops {
		cf := op.CF
		if cf == "" {
			cf = "default"
		}
		cfOps[cf] = append(cfOps[cf], txn.BatchOp{
			Type:  op.Type,
			Key:   op.Key,
			Value: op.Value,
		})
	}

	// Для каждого CF создаём обычный WriteBatch и применяем
	// Нужно гарантировать, что все CF используют один и тот же commit timestamp.
	// Получаем движок для первого CF (любого) и генерируем timestamp от него.
	var firstEngine *engine.LSMEngine
	for cf := range cfOps {
		eng, err := reg.GetCF(cf)
		if err != nil {
			return 0, fmt.Errorf("failed to get CF %q: %w", cf, err)
		}
		firstEngine = eng
		break
	}
	if firstEngine == nil {
		return 0, fmt.Errorf("no operations")
	}

	// Генерируем единый commit timestamp
	commitTS := firstEngine.NextTimestamp()

	// Применяем операции для каждого CF
	for cf, ops := range cfOps {
		eng, err := reg.GetCF(cf)
		if err != nil {
			return 0, fmt.Errorf("failed to get CF %q: %w", cf, err)
		}

		// Создаём обычный WriteBatch для этого CF
		cfBatch := txn.NewWriteBatch()
		for _, op := range ops {
			switch op.Type {
			case txn.OpPut:
				cfBatch.AddPut(op.Key, op.Value)
			case txn.OpDelete:
				cfBatch.AddDelete(op.Key)
			}
		}

		// Применяем с общим commitTS
		if err := txn.ApplyBatchWithTS(eng, cfBatch, commitTS); err != nil {
			// Частичное применение: предыдущие CF уже записали данные.
			// В MVP не откатываем, просто возвращаем ошибку.
			// TODO: реализовать откат.
			return 0, fmt.Errorf("failed to apply batch for CF %q: %w", cf, err)
		}
	}

	return commitTS, nil
}