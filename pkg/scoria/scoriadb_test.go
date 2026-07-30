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

package scoria

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/engine"
)

// ============================================================
// Core Functionality Tests
// ============================================================

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

// ============================================================
// Basic CRUD Operations
// ============================================================

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

	// Deleting a non-existent key should not error (idempotent)
	err = db.Delete([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Delete nonexistent failed: %v", err)
	}
}

// ============================================================
// Scan Operations
// ============================================================

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

// ============================================================
// Column Family Operations
// ============================================================

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

	err = db.DropCF("default")
	if err == nil {
		t.Error("expected error when dropping default CF")
	}
}

// ============================================================
// Batch Operations
// ============================================================

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

// ============================================================
// Transaction Tests
// ============================================================

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

// ============================================================
// Utility Tests
// ============================================================

func TestScoriaDB_ListCFs(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

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

	// Second close should be idempotent
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

// ============================================================
// Edge Cases Tests (Critical for Quality)
// ============================================================

func TestScoriaDB_LargeValue(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Value larger than MaxInlineSize (64 bytes) → should go to VLog
	// This tests the VLog path, which is critical for production.
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

// TestScoriaDB_EmptyValue tests that empty values ([]byte{}) are handled correctly.
// CRITICAL: Empty values are valid and must be distinguishable from nil (tombstone).
// This test ensures MVCC semantics for empty values.
func TestScoriaDB_EmptyValue(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Put an empty value ([]byte{}) — this is NOT a tombstone, it's a valid empty value.
	if err := db.Put([]byte("key"), []byte{}); err != nil {
		t.Fatalf("Put empty value failed: %v", err)
	}

	// Get must return ([]byte{}, nil), NOT (nil, nil).
	// Empty value is different from "key not found" or "key deleted".
	got, err := db.Get([]byte("key"))
	if err != nil {
		t.Fatalf("Get empty value failed: %v", err)
	}
	if got == nil {
		t.Error("expected empty slice ([]byte{}), got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected length 0, got %d", len(got))
	}

	// Additionally, verify that GetLatestInfo correctly distinguishes:
	// - empty value: found=true, ts>0, val=[]byte{}
	// - tombstone: found=false, ts>0, val=nil
	// - not found: found=false, ts=0, val=nil
	eng, _ := db.registry.GetCF("default")
	val, ts, found, err := eng.GetLatestInfo([]byte("key"))
	if err != nil {
		t.Fatalf("GetLatestInfo failed: %v", err)
	}
	if !found {
		t.Errorf("expected found=true for empty value, got found=%v", found)
	}
	if ts == 0 {
		t.Errorf("expected ts>0 for empty value, got ts=%d", ts)
	}
	if val == nil {
		t.Error("expected empty slice ([]byte{}), got nil")
	}
	if len(val) != 0 {
		t.Errorf("expected length 0, got %d", len(val))
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
	// Read before write
	val, err := tx.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "initial" {
		t.Errorf("expected initial, got %s", val)
	}

	// Write inside transaction
	if err := tx.Put([]byte("key"), []byte("tx-value")); err != nil {
		t.Fatal(err)
	}

	// Read after write (should be visible within transaction)
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

	// Verify after commit
	val, err = db.Get([]byte("key"))
	if err != nil {
		t.Fatal(err)
	}
	if string(val) != "tx-value" {
		t.Errorf("expected tx-value after commit, got %s", val)
	}
}

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions("/tmp/test")
	if opts.WorkDir != "/tmp/test" {
		t.Errorf("expected WorkDir /tmp/test, got %s", opts.WorkDir)
	}
	if opts.MemTableSize != 64*1024*1024 {
		t.Errorf("expected MemTableSize 64MB, got %d", opts.MemTableSize)
	}
	if opts.Levels != nil {
		t.Error("expected Levels nil")
	}
	if opts.VFS != nil {
		t.Error("expected VFS nil")
	}
	if opts.WALOptions != nil {
		t.Error("expected WALOptions nil")
	}
}

func TestErrorIterator(t *testing.T) {
	err := fmt.Errorf("test error")
	it := &errorIterator{err: err}

	if it.Next() {
		t.Error("Next() should return false")
	}
	if it.Key() != nil {
		t.Error("Key() should return nil")
	}
	if it.Value() != nil {
		t.Error("Value() should return nil")
	}
	if it.Err() != err {
		t.Errorf("Err() expected %v, got %v", err, it.Err())
	}
	it.Close() // should not panic
}

func TestErrorTransaction(t *testing.T) {
	err := fmt.Errorf("test error")
	tx := &errorTransaction{err: err}

	_, err2 := tx.Get([]byte("key"))
	if err2 != err {
		t.Errorf("Get() expected %v, got %v", err, err2)
	}
	err2 = tx.Put([]byte("key"), []byte("val"))
	if err2 != err {
		t.Errorf("Put() expected %v, got %v", err, err2)
	}
	err2 = tx.Delete([]byte("key"))
	if err2 != err {
		t.Errorf("Delete() expected %v, got %v", err, err2)
	}
	err2 = tx.Commit()
	if err2 != err {
		t.Errorf("Commit() expected %v, got %v", err, err2)
	}
	err2 = tx.Rollback()
	if err2 != nil {
		t.Errorf("Rollback() expected nil, got %v", err2)
	}
}

func TestScoriaDB_ScanLargeValues(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert 100 keys with 4KB values to test scan with large data.
	// This exercises the merge iterator and heap-based scan.
	for i := 0; i < 100; i++ {
		key := []byte(fmt.Sprintf("scan:%05d", i))
		value := make([]byte, 4096)
		if err := db.Put(key, value); err != nil {
			t.Fatal(err)
		}
	}

	iter := db.Scan([]byte("scan:"))
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("Scan error: %v", err)
	}

	if count != 100 {
		t.Errorf("expected 100 keys, got %d", count)
	}
}

func TestScoriaDB_ScanViewRelease(t *testing.T) {
	db, err := NewScoriaDB(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Insert a large value (goes to VLog) to test VLogView release.
	largeValue := make([]byte, 100)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}
	if err := db.Put([]byte("large_key"), largeValue); err != nil {
		t.Fatal(err)
	}

	iter := db.Scan([]byte("large_key"))
	if !iter.Next() {
		t.Fatal("expected at least one key")
	}
	if string(iter.Key()) != "large_key" {
		t.Errorf("expected key 'large_key', got %s", iter.Key())
	}
	if len(iter.Value()) != 100 {
		t.Errorf("expected 100 bytes, got %d", len(iter.Value()))
	}
	iter.Close()

	// After Close(), all VLogViews must be released.
	// Verify by re-scanning — should work without SIGBUS or stale pointers.
	iter2 := db.Scan([]byte("large_key"))
	defer iter2.Close()
	if !iter2.Next() {
		t.Fatal("expected at least one key after re-scan")
	}
	if len(iter2.Value()) != 100 {
		t.Errorf("expected 100 bytes after re-scan, got %d", len(iter2.Value()))
	}
}
