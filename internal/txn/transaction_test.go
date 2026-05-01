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
	"scoriadb/internal/engine"
	"testing"
)

func TestTransactionBasic(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	// Write some initial data
	if err := db.PutWithTS([]byte("key1"), []byte("initial"), 1); err != nil {
		t.Fatalf("failed to put initial: %v", err)
	}

	// Start transaction with startTS = 10
	tx := Begin(db, 10)

	// Read within transaction (should see initial value)
	val, err := tx.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("failed to get key1: %v", err)
	}
	if string(val) != "initial" {
		t.Errorf("expected 'initial', got %s", val)
	}

	// Modify within transaction
	if err := tx.Put([]byte("key1"), []byte("updated")); err != nil {
		t.Fatalf("failed to put in transaction: %v", err)
	}
	if err := tx.Put([]byte("key2"), []byte("new")); err != nil {
		t.Fatalf("failed to put key2: %v", err)
	}

	// Read own writes
	val, err = tx.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("failed to get key1 after put: %v", err)
	}
	if string(val) != "updated" {
		t.Errorf("expected 'updated', got %s", val)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Verify changes are visible after commit (with appropriate snapshot)
	// Since we don't know commitTS, we can't test directly.
	// For now, just ensure transaction is closed.
	if !tx.IsClosed() {
		t.Error("transaction should be closed after commit")
	}
}

func TestTransactionRollback(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	tx := Begin(db, 1)
	if err := tx.Put([]byte("key"), []byte("value")); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	// Rollback
	if err := tx.Rollback(); err != nil {
		t.Fatalf("failed to rollback: %v", err)
	}

	if !tx.IsClosed() {
		t.Error("transaction should be closed after rollback")
	}

	// Ensure write not persisted
	val, err := db.GetWithTS([]byte("key"), 2)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil after rollback, got %s", val)
	}
}

func TestTransactionDelete(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	// Write initial
	if err := db.PutWithTS([]byte("key"), []byte("value"), 1); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	tx := Begin(db, 10)
	// Delete within transaction
	if err := tx.Delete([]byte("key")); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Should see deletion within transaction
	val, err := tx.Get([]byte("key"))
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for deleted key, got %s", val)
	}

	// Commit
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// After commit, deletion should be visible (with appropriate snapshot)
	// Not testing due to unknown commitTS
}

func TestTransactionClosed(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	tx := Begin(db, 1)
	if err := tx.Commit(); err != nil {
		t.Fatalf("failed to commit: %v", err)
	}

	// Operations after commit should fail
	_, err = tx.Get([]byte("key"))
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

func TestTransactionConflict(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	// Запись начального значения
	if err := db.PutWithTS([]byte("key"), []byte("initial"), 1); err != nil {
		t.Fatalf("init put: %v", err)
	}

	// Транзакция A: startTS = 10
	txA := Begin(db, 10)
	if err := txA.Put([]byte("key"), []byte("A")); err != nil {
		t.Fatal(err)
	}

	// Транзакция B: startTS = 10 (такой же)
	txB := Begin(db, 10)
	if err := txB.Put([]byte("key"), []byte("B")); err != nil {
		t.Fatal(err)
	}

	// Коммитим A — должен успешно
	if err := txA.Commit(); err != nil {
		t.Fatalf("txA commit: %v", err)
	}

	// Коммитим B — должен вернуть ErrConflict
	err = txB.Commit()
	if err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}
