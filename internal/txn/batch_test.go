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

func TestWriteBatchBasic(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	batch := NewWriteBatch()
	batch.AddPut([]byte("key1"), []byte("value1"))
	batch.AddPut([]byte("key2"), []byte("value2"))
	batch.AddDelete([]byte("key3"))

	if batch.Size() != 3 {
		t.Errorf("expected batch size 3, got %d", batch.Size())
	}

	// Apply batch
	commitTS, err := ApplyBatch(db, batch)
	if err != nil {
		t.Fatalf("failed to apply batch: %v", err)
	}
	if commitTS == 0 {
		t.Error("expected non-zero commit timestamp")
	}

	// Verify writes
	val, err := db.GetWithTS([]byte("key1"), commitTS)
	if err != nil {
		t.Fatalf("failed to get key1: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("expected value1, got %s", val)
	}

	val, err = db.GetWithTS([]byte("key2"), commitTS)
	if err != nil {
		t.Fatalf("failed to get key2: %v", err)
	}
	if string(val) != "value2" {
		t.Errorf("expected value2, got %s", val)
	}

	// Deleted key should not be found
	val, err = db.GetWithTS([]byte("key3"), commitTS)
	if err != nil {
		t.Fatalf("failed to get key3: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for deleted key, got %s", val)
	}
}

func TestWriteBatchEmpty(t *testing.T) {
	dir := t.TempDir()
	db, err := engine.NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer db.Close()

	batch := NewWriteBatch()
	commitTS, err := ApplyBatch(db, batch)
	if err != nil {
		t.Fatalf("failed to apply empty batch: %v", err)
	}
	if commitTS != 0 {
		t.Errorf("expected commitTS 0 for empty batch, got %d", commitTS)
	}
}

func TestWriteBatchClear(t *testing.T) {
	batch := NewWriteBatch()
	batch.AddPut([]byte("key"), []byte("value"))
	if batch.Size() != 1 {
		t.Fatal("batch size should be 1")
	}
	batch.Clear()
	if batch.Size() != 0 {
		t.Errorf("expected batch size 0 after clear, got %d", batch.Size())
	}
}
