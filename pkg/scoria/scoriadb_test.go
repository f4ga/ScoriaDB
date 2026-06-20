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

// Package scoria provides the public API for ScoriaDB, a high-performance
// embedded LSM-based key-value store with MVCC, Column Families, and
// transaction support.
package scoria

import (
	"bytes"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/engine"
)

func TestNewScoriaDB(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatalf("NewScoriaDB failed: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Error("expected non-nil db")
	}
}

func TestNewScoriaDBWithOptions(t *testing.T) {
	opts := engine.DefaultWALOptions()
	db, err := NewScoriaDBWithOptions(t.TempDir(), opts)
	if err != nil {
		t.Fatalf("NewScoriaDBWithOptions failed: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Error("expected non-nil db")
	}
}

func TestScoriaDB_PutGet(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	key := []byte("test-key")
	value := []byte("test-value")

	if err := db.Put(key, value); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Errorf("Get: expected %q, got %q", value, got)
	}
}

func TestScoriaDB_GetNotFound(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	got, err := db.Get([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %q", got)
	}
}

func TestScoriaDB_Delete(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	key := []byte("to-delete")
	if err := db.Put(key, []byte("value")); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(key); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %q", got)
	}
}

func TestScoriaDB_DeleteNotFound(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	err = db.Delete([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Delete nonexistent failed: %v", err)
	}
}

func TestScoriaDB_Scan(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	keys := []string{"a:1", "a:2", "b:1"}
	for _, k := range keys {
		if err := db.Put([]byte(k), []byte("value")); err != nil {
			t.Fatal(err)
		}
	}

	iter := db.Scan([]byte("a:"))
	defer iter.Close()

	var result []string
	for iter.Next() {
		result = append(result, string(iter.Key()))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 keys, got %d", len(result))
	}
}

func TestScoriaDB_ScanEmpty(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	iter := db.Scan([]byte("nonexistent:"))
	defer iter.Close()

	if iter.Next() {
		t.Error("expected no results")
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
}

func TestScoriaDB_CreateCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "testcf"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	cfs := db.ListCFs()
	found := false
	for _, name := range cfs {
		if name == cfName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("CF %q not found in list: %v", cfName, cfs)
	}
}

func TestScoriaDB_CreateCFDuplicate(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "testcf"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatal(err)
	}

	err = db.CreateCF(cfName)
	if err == nil {
		t.Error("expected error for duplicate CF")
	}
}

func TestScoriaDB_PutGetCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "testcf"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatal(err)
	}

	key := []byte("key")
	value := []byte("value")

	if err := db.PutCF(cfName, key, value); err != nil {
		t.Fatalf("PutCF failed: %v", err)
	}

	got, err := db.GetCF(cfName, key)
	if err != nil {
		t.Fatalf("GetCF failed: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Errorf("expected %q, got %q", value, got)
	}
}

func TestScoriaDB_DeleteCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "testcf"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatal(err)
	}

	key := []byte("key")
	if err := db.PutCF(cfName, key, []byte("value")); err != nil {
		t.Fatal(err)
	}

	if err := db.DeleteCF(cfName, key); err != nil {
		t.Fatalf("DeleteCF failed: %v", err)
	}

	got, err := db.GetCF(cfName, key)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %q", got)
	}
}

func TestScoriaDB_ScanCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "testcf"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatal(err)
	}

	keys := []string{"a:1", "a:2", "b:1"}
	for _, k := range keys {
		if err := db.PutCF(cfName, []byte(k), []byte("value")); err != nil {
			t.Fatal(err)
		}
	}

	iter := db.ScanCF(cfName, []byte("a:"))
	defer iter.Close()

	var result []string
	for iter.Next() {
		result = append(result, string(iter.Key()))
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("ScanCF error: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 keys, got %d", len(result))
	}
}

func TestScoriaDB_NonExistentCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.GetCF("nonexistent", []byte("key"))
	if err == nil {
		t.Error("expected error for non-existent CF")
	}

	err = db.PutCF("nonexistent", []byte("key"), []byte("value"))
	if err == nil {
		t.Error("expected error for non-existent CF")
	}

	err = db.DeleteCF("nonexistent", []byte("key"))
	if err == nil {
		t.Error("expected error for non-existent CF")
	}

	iter := db.ScanCF("nonexistent", nil)
	if iter.Next() {
		t.Error("expected no items from non-existent CF")
	}
	if err := iter.Err(); err == nil {
		t.Error("expected error from non-existent CF iterator")
	}
}

func TestScoriaDB_DropCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "todelete"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatal(err)
	}

	if err := db.DropCF(cfName); err != nil {
		t.Fatalf("DropCF failed: %v", err)
	}

	cfs := db.ListCFs()
	for _, name := range cfs {
		if name == cfName {
			t.Errorf("CF %q still exists after DropCF", cfName)
		}
	}
}

func TestScoriaDB_DropDefaultCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// default CF нельзя удалить
	err = db.DropCF("default")
	if err == nil {
		t.Error("expected error when dropping default CF")
	}
}

func TestScoriaDB_Batch(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	batch := db.NewBatch()
	batch.AddPut([]byte("k1"), []byte("v1"))
	batch.AddPut([]byte("k2"), []byte("v2"))
	batch.AddDelete([]byte("k3"))

	if batch.Size() != 3 {
		t.Errorf("expected size 3, got %d", batch.Size())
	}

	if err := batch.Commit(); err != nil {
		t.Fatalf("Batch Commit failed: %v", err)
	}

	val, err := db.Get([]byte("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "v1" {
		t.Errorf("expected v1, got %s", val)
	}
}

func TestScoriaDB_BatchEmpty(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	batch := db.NewBatch()
	if err := batch.Commit(); err != nil {
		t.Fatalf("empty batch commit failed: %v", err)
	}
	if batch.Size() != 0 {
		t.Errorf("expected size 0, got %d", batch.Size())
	}
}

func TestScoriaDB_BatchClear(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	batch := db.NewBatch()
	batch.AddPut([]byte("key"), []byte("value"))
	if batch.Size() != 1 {
		t.Fatal("expected size 1")
	}
	batch.Clear()
	if batch.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", batch.Size())
	}
}

func TestScoriaDB_BatchForCF(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfName := "testcf"
	if err := db.CreateCF(cfName); err != nil {
		t.Fatal(err)
	}

	batch := db.NewBatchForCF(cfName)
	batch.AddPut([]byte("k1"), []byte("v1"))
	batch.AddDelete([]byte("k2"))

	if err := batch.Commit(); err != nil {
		t.Fatalf("BatchForCF Commit failed: %v", err)
	}

	val, err := db.GetCF(cfName, []byte("k1"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "v1" {
		t.Errorf("expected v1, got %s", val)
	}
}

func TestScoriaDB_BatchForCF_NonExistent(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	batch := db.NewBatchForCF("nonexistent")
	batch.AddPut([]byte("k1"), []byte("v1"))

	err = batch.Commit()
	if err == nil {
		t.Error("expected error for non-existent CF in batch")
	}
}

func TestScoriaDB_Transaction(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("key"), []byte("initial")); err != nil {
		t.Fatal(err)
	}

	tx := db.NewTransaction()
	val, err := tx.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "initial" {
		t.Errorf("expected initial, got %s", val)
	}

	if err := tx.Put([]byte("key"), []byte("updated")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	val, err = db.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "updated" {
		t.Errorf("expected updated, got %s", val)
	}
}

func TestScoriaDB_TransactionRollback(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("key"), []byte("initial")); err != nil {
		t.Fatal(err)
	}

	tx := db.NewTransaction()
	if err := tx.Put([]byte("key"), []byte("rollback")); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	val, err := db.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "initial" {
		t.Errorf("expected initial after rollback, got %s", val)
	}
}

func TestScoriaDB_TransactionClosed(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tx := db.NewTransaction()
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	_, err = tx.Get([]byte("key"))
	if err == nil {
		t.Error("expected error on closed transaction Get")
	}

	err = tx.Put([]byte("key"), []byte("value"))
	if err == nil {
		t.Error("expected error on closed transaction Put")
	}

	err = tx.Delete([]byte("key"))
	if err == nil {
		t.Error("expected error on closed transaction Delete")
	}

	err = tx.Rollback()
	if err == nil {
		t.Error("expected error on closed transaction Rollback")
	}
}

func TestScoriaDB_TransactionConflict(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("key"), []byte("initial")); err != nil {
		t.Fatal(err)
	}

	tx1 := db.NewTransaction()
	tx2 := db.NewTransaction()

	if err := tx1.Put([]byte("key"), []byte("tx1")); err != nil {
		t.Fatal(err)
	}
	if err := tx2.Put([]byte("key"), []byte("tx2")); err != nil {
		t.Fatal(err)
	}

	if err := tx1.Commit(); err != nil {
		t.Fatalf("tx1 Commit failed: %v", err)
	}

	err = tx2.Commit()
	if err == nil {
		t.Error("expected conflict error on tx2 Commit")
	}
}

func TestScoriaDB_ListCFs(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// default всегда есть
	cfs := db.ListCFs()
	if len(cfs) == 0 {
		t.Error("expected at least default CF")
	}

	foundDefault := false
	for _, name := range cfs {
		if name == "default" {
			foundDefault = true
			break
		}
	}
	if !foundDefault {
		t.Error("default CF not found in list")
	}
}

func TestScoriaDB_Close(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}

func TestScoriaDB_CloseTwice(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if err := db.Close(); err != nil {
		t.Fatalf("second Close failed: %v", err)
	}
}

func TestEmbeddedCFDB(t *testing.T) {
	db, err := EmbeddedCFDB(t.TempDir())
	if err != nil {
		t.Fatalf("EmbeddedCFDB failed: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Error("expected non-nil db")
	}
}

func TestOpen(t *testing.T) {
	opts := Options{
		WorkDir: t.TempDir(),
	}
	db, err := Open(opts)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Error("expected non-nil db")
	}
}

func TestOpenWithWALOptions(t *testing.T) {
	walOpts := engine.DefaultWALOptions()
	walOpts.GroupCommitEnabled = false

	opts := Options{
		WorkDir:    t.TempDir(),
		WALOptions: &walOpts,
	}
	db, err := Open(opts)
	if err != nil {
		t.Fatalf("Open with WALOptions failed: %v", err)
	}
	defer db.Close()

	if db == nil {
		t.Error("expected non-nil db")
	}
}

func TestScoriaDB_LargeValue(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Значение больше MaxInlineSize (64 байта) — должно пойти в VLog
	largeValue := make([]byte, 100)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}

	key := []byte("large-key")
	if err := db.Put(key, largeValue); err != nil {
		t.Fatalf("Put large value failed: %v", err)
	}

	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("Get large value failed: %v", err)
	}
	if !bytes.Equal(got, largeValue) {
		t.Errorf("large value mismatch: expected %d bytes, got %d", len(largeValue), len(got))
	}
}

func TestScoriaDB_EmptyKey(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte{}, []byte("value")); err != nil {
		t.Fatalf("Put empty key failed: %v", err)
	}

	got, err := db.Get([]byte{})
	if err != nil {
		t.Fatalf("Get empty key failed: %v", err)
	}
	if string(got) != "value" {
		t.Errorf("expected value, got %s", got)
	}
}

func TestScoriaDB_EmptyValue(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("key"), []byte{}); err != nil {
		t.Fatalf("Put empty value failed: %v", err)
	}

	got, err := db.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get empty value failed: %v", err)
	}
	if got == nil {
		t.Error("expected empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected length 0, got %d", len(got))
	}
}

func TestScoriaDB_TransactionGetBuffer(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.Put([]byte("key"), []byte("initial")); err != nil {
		t.Fatal(err)
	}

	tx := db.NewTransaction()
	// Чтение внутри транзакции до записи
	val, err := tx.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "initial" {
		t.Errorf("expected initial, got %s", val)
	}

	// Запись внутри транзакции
	if err := tx.Put([]byte("key"), []byte("tx-value")); err != nil {
		t.Fatal(err)
	}

	// Чтение после записи (должна быть видна)
	val, err = tx.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "tx-value" {
		t.Errorf("expected tx-value after put, got %s", val)
	}

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Проверка после коммита
	val, err = db.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "tx-value" {
		t.Errorf("expected tx-value after commit, got %s", val)
	}
}
