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
	"testing"
)

// mockEngine implements Engine for testing.
type mockEngine struct {
	nextTS uint64
	// minActiveSnapshotTS is the minimum TS among registered snapshots.
	// Updated by RegisterSnapshot/UnregisterSnapshot. Not concurrency-safe; tests
	// use it single-threaded.
	minActiveSnapshotTS uint64
}

func (m *mockEngine) NextTimestamp() uint64 {
	m.nextTS++
	return m.nextTS
}

func (m *mockEngine) WriteAtomicBatch(data []byte, commitTS uint64) error {
	return nil
}

func (m *mockEngine) RegisterSnapshot(snapshotTS uint64) {
	if m.minActiveSnapshotTS == 0 || snapshotTS < m.minActiveSnapshotTS {
		m.minActiveSnapshotTS = snapshotTS
	}
}

func (m *mockEngine) UnregisterSnapshot(snapshotTS uint64) {
	// Single-transaction mock: only tracks a single min. When unregistering the
	// min TS, reset to 0. Used to verify snapshot registration on Begin.
	if m.minActiveSnapshotTS == snapshotTS {
		m.minActiveSnapshotTS = 0
	}
}

func (m *mockEngine) CheckConflict(key []byte, startTS uint64) (bool, error) {
	return false, nil
}

func (m *mockEngine) GetWithTS(key []byte, snapshotTS uint64) ([]byte, error) {
	return nil, nil
}

func TestTransactionLifecycle(t *testing.T) {
	db := &mockEngine{nextTS: 100}

	tx := Begin(db, 100)
	if tx.IsClosed() {
		t.Error("new transaction should not be closed")
	}
	if tx.StartTS() != 100 {
		t.Errorf("expected startTS 100, got %d", tx.StartTS())
	}

	// Put and Get within transaction
	if err := tx.Put([]byte("key"), []byte("value")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	val, err := tx.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "value" {
		t.Errorf("expected 'value', got %s", val)
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if !tx.IsClosed() {
		t.Error("transaction should be closed after commit")
	}
}

func TestTransactionRollbackUnit(t *testing.T) {
	db := &mockEngine{nextTS: 100}
	tx := Begin(db, 100)

	if err := tx.Put([]byte("key"), []byte("value")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if !tx.IsClosed() {
		t.Error("transaction should be closed after rollback")
	}
}

func TestTransactionClosedUnit(t *testing.T) {
	db := &mockEngine{nextTS: 100}
	tx := Begin(db, 100)

	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	// Operations after commit should fail
	_, err := tx.Get([]byte("key"))
	if err != ErrTransactionClosed {
		t.Errorf("expected ErrTransactionClosed, got %v", err)
	}
	err = tx.Put([]byte("key"), []byte("value"))
	if err != ErrTransactionClosed {
		t.Errorf("expected ErrTransactionClosed, got %v", err)
	}
	err = tx.Delete([]byte("key"))
	if err != ErrTransactionClosed {
		t.Errorf("expected ErrTransactionClosed, got %v", err)
	}
	err = tx.Commit()
	if err != ErrTransactionClosed {
		t.Errorf("expected ErrTransactionClosed on second commit, got %v", err)
	}
	err = tx.Rollback()
	if err != ErrTransactionClosed {
		t.Errorf("expected ErrTransactionClosed on rollback after commit, got %v", err)
	}
}

func TestBeginWithNextTS(t *testing.T) {
	db := &mockEngine{nextTS: 50}
	tx, err := BeginWithNextTS(db)
	if err != nil {
		t.Fatalf("BeginWithNextTS failed: %v", err)
	}
	if tx.StartTS() != 51 {
		t.Errorf("expected startTS 51, got %d", tx.StartTS())
	}
}

// TestTransactionSnapshotRegistration verifies that opening a transaction
// registers its snapshot with the engine, and closing it (Commit or Rollback)
// unregisters it. This guarantees compaction does not discard versions needed
// by an active transaction.
func TestTransactionSnapshotRegistration(t *testing.T) {
	db := &mockEngine{nextTS: 0}

	// Initially no active snapshots.
	if min := db.minActiveSnapshotTS; min != 0 {
		t.Fatalf("expected no active snapshot, got %d", min)
	}

	// Begin must register the snapshot.
	tx := Begin(db, 100)
	if min := db.minActiveSnapshotTS; min != 100 {
		t.Errorf("expected minActiveSnapshotTS 100 after Begin, got %d", min)
	}

	// Commit must unregister the snapshot.
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if min := db.minActiveSnapshotTS; min != 0 {
		t.Errorf("expected minActiveSnapshotTS 0 after Commit, got %d", min)
	}

	// Begin again, then Rollback must also unregister.
	tx2 := Begin(db, 200)
	if min := db.minActiveSnapshotTS; min != 200 {
		t.Errorf("expected minActiveSnapshotTS 200 after second Begin, got %d", min)
	}
	if err := tx2.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}
	if min := db.minActiveSnapshotTS; min != 0 {
		t.Errorf("expected minActiveSnapshotTS 0 after Rollback, got %d", min)
	}

	// BeginWithNextTS inherits registration via Begin.
	tx3, err := BeginWithNextTS(db)
	if err != nil {
		t.Fatalf("BeginWithNextTS failed: %v", err)
	}
	if got := tx3.StartTS(); got != 1 {
		t.Errorf("expected startTS 1, got %d", got)
	}
	if min := db.minActiveSnapshotTS; min != 1 {
		t.Errorf("expected minActiveSnapshotTS 1 after BeginWithNextTS, got %d", min)
	}
}
