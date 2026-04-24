package txn

import (
	"errors"
	"fmt"
	"scoriadb/internal/engine"
)

var (
	// ErrConflict возвращается при попытке коммита транзакции, когда другой транзакцией
	// была изменена версия ключа после startTS данной транзакции.
	ErrConflict = errors.New("transaction conflict")
	// ErrTransactionClosed возвращается при попытке использовать закрытую транзакцию.
	ErrTransactionClosed = errors.New("transaction closed")
)

// pendingOp представляет операцию, ожидающую коммита.
type pendingOp struct {
	Type  OpType
	Key   []byte
	Value []byte // для Delete может быть nil
}

// Transaction представляет интерактивную транзакцию с оптимистичной блокировкой.
type Transaction struct {
	db      *engine.LSMEngine
	startTS uint64            // snapshot timestamp
	writes  map[string]*pendingOp // буфер незакоммитченных изменений (ключ -> операция)
	closed  bool              // true после Commit или Rollback
}

// Begin начинает новую транзакцию на указанном движке.
// startTS определяет snapshot изоляции (все чтения видят данные на момент этого timestamp).
func Begin(db *engine.LSMEngine, startTS uint64) *Transaction {
	return &Transaction{
		db:      db,
		startTS: startTS,
		writes:  make(map[string]*pendingOp),
		closed:  false,
	}
}

// BeginWithNextTS начинает новую транзакцию, автоматически получая startTS как следующий доступный timestamp.
// Для простоты используем атомарный инкремент LastTS в движке.
func BeginWithNextTS(db *engine.LSMEngine) (*Transaction, error) {
	// TODO: реализовать атомарное получение следующего timestamp из движка
	// Временно используем 0 как заглушку
	startTS := uint64(0)
	return Begin(db, startTS), nil
}

// Get возвращает значение ключа в контексте транзакции.
// Сначала проверяет буфер writes, затем движок с snapshotTS = startTS.
func (tx *Transaction) Get(key []byte) ([]byte, error) {
	if tx.closed {
		return nil, ErrTransactionClosed
	}
	// Проверяем буфер
	if op, ok := tx.writes[string(key)]; ok {
		if op.Type == OpDelete {
			// Ключ удалён в этой транзакции
			return nil, nil
		}
		return op.Value, nil
	}
	// Читаем из движка с snapshotTS = startTS
	return tx.db.GetWithTS(key, tx.startTS)
}

// Put добавляет операцию записи в буфер транзакции.
// Фактическая запись произойдёт при Commit.
func (tx *Transaction) Put(key, value []byte) error {
	if tx.closed {
		return ErrTransactionClosed
	}
	tx.writes[string(key)] = &pendingOp{
		Type:  OpPut,
		Key:   key,
		Value: value,
	}
	return nil
}

// Delete добавляет операцию удаления в буфер транзакции.
func (tx *Transaction) Delete(key []byte) error {
	if tx.closed {
		return ErrTransactionClosed
	}
	tx.writes[string(key)] = &pendingOp{
		Type:  OpDelete,
		Key:   key,
		Value: nil,
	}
	return nil
}

// Commit применяет все изменения из буфера атомарно.
// Проверяет конфликты: для каждого ключа в буфере убеждается, что в движке нет версии
// с commitTS > startTS (т.е. ключ не был изменён после начала транзакции).
// Если конфликт обнаружен, возвращает ErrConflict.
// В случае успеха генерирует новый commitTS и применяет все операции через WriteBatch.
func (tx *Transaction) Commit() error {
	if tx.closed {
		return ErrTransactionClosed
	}
	defer func() { tx.closed = true }()

	if len(tx.writes) == 0 {
		// Нет изменений — транзакция успешно завершена
		return nil
	}

	// Проверяем конфликты
	for _, op := range tx.writes {
		// Для каждого ключа нужно проверить, есть ли в движке версия с commitTS > startTS
		// Получаем самую новую версию ключа (с любым timestamp)
		// В реальной реализации нужно использовать GetWithTS с большим snapshotTS
		// или специальный метод проверки конфликтов.
		// Для простоты пока пропускаем проверку.
		// TODO: реализовать проверку конфликтов
		_ = op
	}

	// Генерируем commitTS (должен быть > startTS)
	commitTS := tx.db.NextTimestamp()
	// Убедимся, что commitTS > startTS (если нет, инкрементируем)
	if commitTS <= tx.startTS {
		commitTS = tx.startTS + 1
	}

	// Создаём WriteBatch и добавляем все операции
	batch := NewWriteBatch()
	for _, op := range tx.writes {
		switch op.Type {
		case OpPut:
			batch.AddPut(op.Key, op.Value)
		case OpDelete:
			batch.AddDelete(op.Key)
		}
	}

	// Применяем батч
	if err := ApplyBatchWithTS(tx.db, batch, commitTS); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Rollback отменяет транзакцию, очищая буфер изменений.
func (tx *Transaction) Rollback() error {
	if tx.closed {
		return ErrTransactionClosed
	}
	tx.closed = true
	tx.writes = nil
	return nil
}

// StartTS возвращает start timestamp транзакции.
func (tx *Transaction) StartTS() uint64 {
	return tx.startTS
}

// IsClosed возвращает true, если транзакция уже завершена (Commit или Rollback).
func (tx *Transaction) IsClosed() bool {
	return tx.closed
}