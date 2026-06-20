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

func TestEncodeDecodeBatch(t *testing.T) {
	batch := NewWriteBatch()
	batch.AddPut([]byte("key1"), []byte("value1"))
	batch.AddPut([]byte("key2"), []byte("value2"))
	batch.AddDelete([]byte("key3"))

	encoded, err := EncodeBatch(batch)
	if err != nil {
		t.Fatalf("EncodeBatch failed: %v", err)
	}

	decoded, err := DecodeBatch(encoded)
	if err != nil {
		t.Fatalf("DecodeBatch failed: %v", err)
	}

	if decoded.Size() != batch.Size() {
		t.Errorf("size mismatch: expected %d, got %d", batch.Size(), decoded.Size())
	}
}

func TestDecodeBatchEmpty(t *testing.T) {
	data := []byte{0, 0} // 0 ops
	batch, err := DecodeBatch(data)
	if err != nil {
		t.Fatalf("DecodeBatch failed: %v", err)
	}
	if batch.Size() != 0 {
		t.Errorf("expected 0 ops, got %d", batch.Size())
	}
}

func TestDecodeBatchInvalid(t *testing.T) {
	// Too short data
	_, err := DecodeBatch([]byte{0})
	if err == nil {
		t.Error("expected error for too short data")
	}

	// Malformed data
	_, err = DecodeBatch([]byte{0, 1, 255})
	if err == nil {
		t.Error("expected error for malformed data")
	}
}
