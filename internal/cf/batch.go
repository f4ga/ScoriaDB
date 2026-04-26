package cf

import (
	"fmt"
	"scoriadb/internal/engine"
	"scoriadb/internal/txn"
)

// CFBatchOp represents a batch operation with a Column Family specification.
type CFBatchOp struct {
	CF    string
	Type  txn.OpType
	Key   []byte
	Value []byte // may be nil for Delete
}

// CFWriteBatch represents an atomic batch of operations that may span multiple CFs.
type CFWriteBatch struct {
	ops []CFBatchOp
}

// NewCFWriteBatch creates a new empty CFWriteBatch.
func NewCFWriteBatch() *CFWriteBatch {
	return &CFWriteBatch{
		ops: make([]CFBatchOp, 0),
	}
}

// AddPut adds a put operation to the specified CF.
func (b *CFWriteBatch) AddPut(cf string, key, value []byte) {
	b.ops = append(b.ops, CFBatchOp{
		CF:    cf,
		Type:  txn.OpPut,
		Key:   key,
		Value: value,
	})
}

// AddDelete adds a delete operation to the specified CF.
func (b *CFWriteBatch) AddDelete(cf string, key []byte) {
	b.ops = append(b.ops, CFBatchOp{
		CF:    cf,
		Type:  txn.OpDelete,
		Key:   key,
		Value: nil,
	})
}

// Size returns the number of operations in the batch.
func (b *CFWriteBatch) Size() int {
	return len(b.ops)
}

// Clear clears the batch.
func (b *CFWriteBatch) Clear() {
	b.ops = b.ops[:0]
}

// ApplyCFBatch applies all batch operations atomically to the CF registry.
// Returns the commit timestamp used for all operations.
// If an error occurs, no operation should be applied.
// Algorithm:
// 1. Group operations by CF.
// 2. For each CF, obtain the engine via the registry.
// 3. For each engine, create a plain WriteBatch (without CF) and apply it.
// 4. Atomicity across all CFs is important: if any operation fails, the whole batch must roll back.
// For MVP simplicity we apply operations sequentially and roll back on error by undoing previous operations (which is complex).
// Alternatively, a two‑phase commit with WAL could be used, but for now we just return an error after partial application.
// Tracked for Release 2: improve atomicity.
func ApplyCFBatch(reg *Registry, batch *CFWriteBatch) (uint64, error) {
	if batch.Size() == 0 {
		return 0, nil
	}

	// Group operations by CF
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

	// For each CF create a plain WriteBatch and apply
	// Ensure all CFs use the same commit timestamp.
	// Get the engine of the first CF (any) and generate a timestamp from it.
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

	// Generate a unified commit timestamp
	commitTS := firstEngine.NextTimestamp()

	// Apply operations for each CF
	for cf, ops := range cfOps {
		eng, err := reg.GetCF(cf)
		if err != nil {
			return 0, fmt.Errorf("failed to get CF %q: %w", cf, err)
		}

		// Create a plain WriteBatch for this CF
		cfBatch := txn.NewWriteBatch()
		for _, op := range ops {
			switch op.Type {
			case txn.OpPut:
				cfBatch.AddPut(op.Key, op.Value)
			case txn.OpDelete:
				cfBatch.AddDelete(op.Key)
			}
		}

		// Apply with the shared commitTS
		if err := txn.ApplyBatchWithTS(eng, cfBatch, commitTS); err != nil {
			// Partial application: previous CFs have already written data.
			// In MVP we do not roll back, just return an error.
			// Tracked for Release 2: implement rollback.
			return 0, fmt.Errorf("failed to apply batch for CF %q: %w", cf, err)
		}
	}

	return commitTS, nil
}