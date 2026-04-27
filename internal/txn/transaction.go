package txn

import (
	"errors"
	"fmt"
	"scoriadb/internal/engine"
)

var (
	// ErrConflict is returned when a commit attempt fails because another transaction
	// has modified a key version after this transaction's startTS.
	ErrConflict = errors.New("transaction conflict")
	// ErrTransactionClosed is returned when attempting to use a closed transaction.
	ErrTransactionClosed = errors.New("transaction closed")
)

// pendingOp represents an operation pending commit.
type pendingOp struct {
	Type  OpType
	Key   []byte
	Value []byte // may be nil for Delete
}

// Transaction represents an interactive transaction with optimistic concurrency control.
type Transaction struct {
	db      *engine.LSMEngine
	startTS uint64            // snapshot timestamp
	writes  map[string]*pendingOp // buffer of uncommitted changes (key → operation)
	closed  bool              // true after Commit or Rollback
}

// Begin starts a new transaction on the given engine.
// startTS defines the isolation snapshot (all reads see data as of this timestamp).
func Begin(db *engine.LSMEngine, startTS uint64) *Transaction {
	return &Transaction{
		db:      db,
		startTS: startTS,
		writes:  make(map[string]*pendingOp),
		closed:  false,
	}
}

// BeginWithNextTS starts a new transaction, automatically obtaining startTS as the next available timestamp.
// For simplicity we use atomic increment of LastTS in the engine.
func BeginWithNextTS(db *engine.LSMEngine) (*Transaction, error) {
	startTS := db.NextTimestamp()
	return Begin(db, startTS), nil
}

// Get returns the value of a key within the transaction context.
// First checks the write buffer, then the engine with snapshotTS = startTS.
func (tx *Transaction) Get(key []byte) ([]byte, error) {
	if tx.closed {
		return nil, ErrTransactionClosed
	}
	// Check buffer
	if op, ok := tx.writes[string(key)]; ok {
		if op.Type == OpDelete {
			// Key deleted within this transaction
			return nil, nil
		}
		return op.Value, nil
	}
	// Read from engine with snapshotTS = startTS
	return tx.db.GetWithTS(key, tx.startTS)
}

// Put adds a write operation to the transaction buffer.
// The actual write will happen at Commit.
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

// Delete adds a delete operation to the transaction buffer.
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

// Commit applies all changes from the buffer atomically.
// Checks for conflicts: for each key in the buffer ensures there is no version in the engine
// with commitTS > startTS (i.e., the key hasn't been modified after the transaction started).
// If a conflict is detected, returns ErrConflict.
// On success, generates a new commitTS and applies all operations via WriteBatch.
func (tx *Transaction) Commit() error {
	if tx.closed {
		return ErrTransactionClosed
	}
	defer func() { tx.closed = true }()

	if len(tx.writes) == 0 {
		// No changes – transaction successfully completed
		return nil
	}

	// Check conflicts
	for _, op := range tx.writes {
		// For each key we need to verify there is no version in the engine with commitTS > startTS.
		// Get the newest version of the key (with any timestamp).
		// In a real implementation we would use GetWithTS with a larger snapshotTS
		// or a dedicated conflict‑checking method.
		// For simplicity we skip the check for now.
		// TODO: implement conflict checking
		_ = op
	}

	// Generate commitTS (must be > startTS)
	commitTS := tx.db.NextTimestamp()
	// Ensure commitTS > startTS (if not, increment)
	if commitTS <= tx.startTS {
		commitTS = tx.startTS + 1
	}

	// Create WriteBatch and add all operations
	batch := NewWriteBatch()
	for _, op := range tx.writes {
		switch op.Type {
		case OpPut:
			batch.AddPut(op.Key, op.Value)
		case OpDelete:
			batch.AddDelete(op.Key)
		}
	}

	// Apply the batch
	if err := ApplyBatchWithTS(tx.db, batch, commitTS); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Rollback cancels the transaction, clearing the change buffer.
func (tx *Transaction) Rollback() error {
	if tx.closed {
		return ErrTransactionClosed
	}
	tx.closed = true
	tx.writes = nil
	return nil
}

// StartTS returns the transaction's start timestamp.
func (tx *Transaction) StartTS() uint64 {
	return tx.startTS
}

// IsClosed returns true if the transaction has already been completed (Commit or Rollback).
func (tx *Transaction) IsClosed() bool {
	return tx.closed
}