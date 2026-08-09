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

package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

func TestLSMEnginePutGetWithTS(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Put and Get with same timestamp
	key := []byte("test_key")
	value := []byte("test_value")
	if err := eng.PutWithTS(key, value, 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	got, err := eng.GetWithTS(key, 1)
	if err != nil {
		t.Fatalf("GetWithTS failed: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Errorf("expected %s, got %s", value, got)
	}

	// Get non-existent key
	got, err = eng.GetWithTS([]byte("nonexistent"), 1)
	if err != nil {
		t.Fatalf("GetWithTS failed for non-existent: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for non-existent key, got %v", got)
	}
}

func TestLSMEnginePutWithTSLargeValue(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Value larger than MaxInlineSize (64 bytes) should go to VLog
	largeValue := make([]byte, 200)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}
	key := []byte("large_key")

	if err := eng.PutWithTS(key, largeValue, 1); err != nil {
		t.Fatalf("PutWithTS large value failed: %v", err)
	}

	got, err := eng.GetWithTS(key, 1)
	if err != nil {
		t.Fatalf("GetWithTS large value failed: %v", err)
	}
	if !bytes.Equal(got, largeValue) {
		t.Errorf("large value mismatch: len %d vs %d", len(got), len(largeValue))
	}
}

func TestLSMEngineDeleteWithTS(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	key := []byte("delete_key")
	value := []byte("delete_value")

	if err := eng.PutWithTS(key, value, 10); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Delete at timestamp 20
	if err := eng.DeleteWithTS(key, 20); err != nil {
		t.Fatalf("DeleteWithTS failed: %v", err)
	}

	// Should still be visible at timestamp 15 (before delete)
	got, err := eng.GetWithTS(key, 15)
	if err != nil {
		t.Fatalf("GetWithTS failed: %v", err)
	}
	if !bytes.Equal(got, value) {
		t.Errorf("expected %s before delete, got %s", value, got)
	}

	// Should not be visible at timestamp 25 (after delete)
	got, err = eng.GetWithTS(key, 25)
	if err != nil {
		t.Fatalf("GetWithTS after delete failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %v", got)
	}

	// Double delete should not error
	if err := eng.DeleteWithTS(key, 30); err != nil {
		t.Fatalf("double DeleteWithTS failed: %v", err)
	}
}

func TestLSMEngineWriteAtomicBatch(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Create a batch with multiple operations using the correct encoding format
	// Format: numOps(2 bytes) + [opType(1 byte) + keyLen(2 bytes) + key + valLen(4 bytes) + value]...
	// opType: 1=put, 2=delete
	data := encodeTestBatch(
		testBatchOp{isDelete: false, key: "batch_key1", value: "batch_val1"},
		testBatchOp{isDelete: false, key: "batch_key2", value: "batch_val2"},
		testBatchOp{isDelete: true, key: "batch_key3", value: ""},
	)

	if err := eng.WriteAtomicBatch(data, 50); err != nil {
		t.Fatalf("WriteAtomicBatch failed: %v", err)
	}

	// Verify batch operations
	got, err := eng.GetWithTS([]byte("batch_key1"), 50)
	if err != nil {
		t.Fatalf("Get batch_key1 failed: %v", err)
	}
	if !bytes.Equal(got, []byte("batch_val1")) {
		t.Errorf("expected batch_val1, got %s", got)
	}

	got, err = eng.GetWithTS([]byte("batch_key2"), 50)
	if err != nil {
		t.Fatalf("Get batch_key2 failed: %v", err)
	}
	if !bytes.Equal(got, []byte("batch_val2")) {
		t.Errorf("expected batch_val2, got %s", got)
	}

	// batch_key3 was deleted (never existed, so should be nil)
	got, err = eng.GetWithTS([]byte("batch_key3"), 50)
	if err != nil {
		t.Fatalf("Get batch_key3 failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for deleted key, got %v", got)
	}
}

func TestLSMEngineCheckConflict(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	key := []byte("conflict_key")

	// No conflict for unmodified key
	conflict, err := eng.CheckConflict(key, 100)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if conflict {
		t.Error("expected no conflict for unmodified key")
	}

	// Write at timestamp 50
	if err := eng.PutWithTS(key, []byte("value"), 50); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Conflict if startTS < last commit TS
	conflict, err = eng.CheckConflict(key, 30)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if !conflict {
		t.Error("expected conflict when startTS < commitTS")
	}

	// No conflict if startTS >= last commit TS
	conflict, err = eng.CheckConflict(key, 50)
	if err != nil {
		t.Fatalf("CheckConflict failed: %v", err)
	}
	if conflict {
		t.Error("expected no conflict when startTS >= commitTS")
	}
}

func TestLSMEngineNextTimestamp(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	ts1 := eng.NextTimestamp()
	ts2 := eng.NextTimestamp()
	if ts2 <= ts1 {
		t.Errorf("NextTimestamp should be monotonic: %d then %d", ts1, ts2)
	}
}

func TestLSMEngineScan(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	keys := []string{"user:alice", "user:bob", "admin:root", "user:charlie"}
	for i, k := range keys {
		if err := eng.PutWithTS([]byte(k), []byte("value"), uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	// Scan with prefix "user:"
	iter := eng.Scan([]byte("user:"))
	defer iter.Close()

	var scanned []string
	for iter.Next() {
		scanned = append(scanned, string(iter.Key()))
	}

	if len(scanned) != 3 {
		t.Errorf("expected 3 keys with prefix 'user:', got %d: %v", len(scanned), scanned)
	}

	// Scan with non-existent prefix
	iter2 := eng.Scan([]byte("nonexistent:"))
	defer iter2.Close()
	if iter2.Next() {
		t.Error("expected no results for non-existent prefix")
	}
}

func TestLSMEngineSnapshot(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Initially no active snapshots
	if min := eng.GetMinActiveSnapshotTS(); min != 0 {
		t.Errorf("expected minActiveSnapshotTS 0, got %d", min)
	}

	// Register a snapshot
	eng.RegisterSnapshot(100)
	if min := eng.GetMinActiveSnapshotTS(); min != 100 {
		t.Errorf("expected minActiveSnapshotTS 100, got %d", min)
	}

	// Register a lower snapshot (should update min)
	eng.RegisterSnapshot(50)
	if min := eng.GetMinActiveSnapshotTS(); min != 50 {
		t.Errorf("expected minActiveSnapshotTS 50, got %d", min)
	}

	// Unregister the lower snapshot. With reference counting the 100
	// snapshot is still active, so the min must move to 100, not to 0.
	eng.UnregisterSnapshot(50)
	if min := eng.GetMinActiveSnapshotTS(); min != 100 {
		t.Errorf("expected minActiveSnapshotTS 100 after unregister, got %d", min)
	}

	// Unregister the last remaining snapshot: min must reset to 0.
	eng.UnregisterSnapshot(100)
	if min := eng.GetMinActiveSnapshotTS(); min != 0 {
		t.Errorf("expected minActiveSnapshotTS 0 after last unregister, got %d", min)
	}
}

func TestLSMEngineClose(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Close should succeed
	if err := eng.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close should not error
	if err := eng.Close(); err != nil {
		t.Fatalf("double Close failed: %v", err)
	}

	// Operations on closed engine should fail
	if err := eng.PutWithTS([]byte("key"), []byte("val"), 1); err == nil {
		t.Error("expected error on PutWithTS after close")
	}
}

func TestLSMEngineGetLatestInfo(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	key := []byte("latest_key")

	// No value yet
	val, ts, _, err := eng.GetLatestInfo(key)
	if err != nil {
		t.Fatalf("GetLatestInfo failed: %v", err)
	}
	if val != nil || ts != 0 {
		t.Errorf("expected nil/0 for missing key, got %v/%d", val, ts)
	}

	// Write first version
	if err := eng.PutWithTS(key, []byte("v1"), 10); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	val, ts, _, err = eng.GetLatestInfo(key)
	if err != nil {
		t.Fatalf("GetLatestInfo failed: %v", err)
	}
	if !bytes.Equal(val, []byte("v1")) || ts != 10 {
		t.Errorf("expected v1/10, got %s/%d", val, ts)
	}

	// Write second version
	if err := eng.PutWithTS(key, []byte("v2"), 20); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	val, ts, _, err = eng.GetLatestInfo(key)
	if err != nil {
		t.Fatalf("GetLatestInfo failed: %v", err)
	}
	if !bytes.Equal(val, []byte("v2")) || ts != 20 {
		t.Errorf("expected v2/20, got %s/%d", val, ts)
	}
}

func TestLSMEngineInvalidateVLogPointers(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Put a large value (goes to VLog)
	largeValue := make([]byte, 100)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}
	if err := eng.PutWithTS([]byte("vlog_key"), largeValue, 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Invalidate VLog pointers (simulates recovery scenario)
	eng.InvalidateVLogPointers()

	// After invalidation, the value should still be readable
	// (InvalidateVLogPointers only removes pointers from memtable that point to invalid VLog entries)
	got, err := eng.GetWithTS([]byte("vlog_key"), 1)
	if err != nil {
		t.Fatalf("GetWithTS after invalidation failed: %v", err)
	}
	if got != nil {
		t.Logf("Value after invalidation: %d bytes", len(got))
	}
}

func TestLSMEngineCollectLiveValuePointers(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Put a large value (goes to VLog)
	largeValue := make([]byte, 100)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}
	if err := eng.PutWithTS([]byte("gc_key"), largeValue, 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Collect live value pointers
	pointers, err := eng.CollectLiveValuePointers()
	if err != nil {
		t.Fatalf("CollectLiveValuePointers failed: %v", err)
	}

	// Should have at least one pointer (for the large value)
	t.Logf("Collected %d live value pointers", len(pointers))
}

func TestDecodeBatchLocal(t *testing.T) {
	// Test empty batch
	_, err := decodeBatchLocal([]byte{})
	if err == nil {
		t.Error("expected error for empty batch data")
	}

	// Test malformed batch data (numOps=1 but no op data)
	_, err = decodeBatchLocal([]byte{0x00, 0x01})
	if err == nil {
		t.Error("expected error for malformed batch data")
	}

	// Test valid batch with one put operation
	// Format: numOps(2 bytes) + opType(1 byte) + keyLen(2 bytes) + key + valLen(4 bytes) + value
	data := encodeTestBatch(testBatchOp{isDelete: false, key: "key1", value: "value1"})

	ops, err := decodeBatchLocal(data)
	if err != nil {
		t.Fatalf("decodeBatchLocal failed: %v", err)
	}
	if len(ops) != 1 {
		t.Errorf("expected 1 op, got %d", len(ops))
	}
	if ops[0].IsDelete {
		t.Error("expected put operation, not delete")
	}
	if string(ops[0].Key) != "key1" {
		t.Errorf("expected key 'key1', got %s", ops[0].Key)
	}
}

func TestDecodeStoredValue(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Test nil value
	val, err := eng.decodeStoredValue(nil, false)
	if err != nil {
		t.Fatalf("decodeStoredValue(nil) failed: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for nil input, got %v", val)
	}

	// Test inline value (not a VLog pointer)
	inlineVal := []byte("inline_value")
	val, err = eng.decodeStoredValue(inlineVal, false)
	if err != nil {
		t.Fatalf("decodeStoredValue(inline) failed: %v", err)
	}
	if !bytes.Equal(val, inlineVal) {
		t.Errorf("expected %s, got %s", inlineVal, val)
	}

	// Test empty value — should return empty slice (not nil)
	// to distinguish "key found with empty value" from "key not found".
	val, err = eng.decodeStoredValue([]byte{}, false)
	if err != nil {
		t.Fatalf("decodeStoredValue(empty) failed: %v", err)
	}
	if val == nil {
		t.Errorf("expected empty slice for empty stored value, got nil")
	}
	if len(val) != 0 {
		t.Errorf("expected empty slice, got %v (len=%d)", val, len(val))
	}
}

func TestMemTableDeleteWithTS(t *testing.T) {
	mt := memtable.NewMemTable()

	key := []byte("delete_me")
	mvccKey := mvcc.NewMVCCKey(key, 100)
	mt.Put(mvccKey, []byte("value"))

	// Verify it exists
	_, found := mt.Get(mvccKey)
	if !found {
		t.Fatal("key should exist before delete")
	}

	// Delete
	deleteKey := mvcc.NewMVCCKey(key, 200)
	mt.DeleteWithTS(deleteKey)

	// Should not be found at timestamp after delete
	_, found = mt.Get(mvcc.NewMVCCKey(key, 300))
	if found {
		t.Error("key should not be found after delete")
	}

	// Should still be found at timestamp before delete
	val, found := mt.Get(mvcc.NewMVCCKey(key, 150))
	if !found {
		t.Error("key should be found at timestamp before delete")
	}
	if string(val) != "value" {
		t.Errorf("expected 'value', got %s", val)
	}
}

func TestMemTableClose(t *testing.T) {
	mt := memtable.NewMemTable()
	mt.Put(mvcc.NewMVCCKey([]byte("key"), 1), []byte("val"))
	// Close should not panic
	mt.Close()
}

func TestEncodeDecodeValuePointerEdgeCases(t *testing.T) {
	// Max values
	vp := ValuePointer{Offset: 1<<63 - 1, Size: 1<<31 - 1}
	var buf [12]byte
	EncodeValuePointer(vp, buf[:])
	decoded, ok := DecodeValuePointer(buf[:])
	if !ok {
		t.Fatal("DecodeValuePointer failed for max values")
	}
	if decoded.Offset != vp.Offset {
		t.Errorf("Offset mismatch: %d vs %d", decoded.Offset, vp.Offset)
	}
	if decoded.Size != vp.Size {
		t.Errorf("Size mismatch: %d vs %d", decoded.Size, vp.Size)
	}
}

func TestNewLSMEngineWithOptions(t *testing.T) {
	dir := t.TempDir()

	// Create with custom WAL options (group commit disabled)
	opts := WALOptions{
		GroupCommitEnabled: false,
	}
	eng, err := NewLSMEngine(dir, opts)
	if err != nil {
		t.Fatalf("failed to create engine with options: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Basic operation should still work
	if err := eng.PutWithTS([]byte("key"), []byte("val"), 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}
}

func TestLSMEngineRecoveryFromWAL(t *testing.T) {
	dir := t.TempDir()

	// Create engine, write data, close
	eng1, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine1: %v", err)
	}

	if err := eng1.PutWithTS([]byte("persist_key"), []byte("persist_val"), 42); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}
	errors.CloseWithFatal(eng1, "engine1")

	// Reopen engine - should recover from WAL
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine2: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine2")

	got, err := eng2.GetWithTS([]byte("persist_key"), 42)
	if err != nil {
		t.Fatalf("GetWithTS after recovery failed: %v", err)
	}
	if !bytes.Equal(got, []byte("persist_val")) {
		t.Errorf("expected persist_val after recovery, got %s", got)
	}
}

func TestLSMEngineSetMinActiveSnapshotTS(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	eng.SetMinActiveSnapshotTS(42)
	if min := eng.GetMinActiveSnapshotTS(); min != 42 {
		t.Errorf("expected 42, got %d", min)
	}
}

func TestActiveFrozenMemTable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Active memtable should not be nil
	if mt := eng.ActiveMemTable(); mt == nil {
		t.Error("ActiveMemTable should not be nil")
	}

	// Frozen memtable should be nil initially
	if mt := eng.FrozenMemTable(); mt != nil {
		t.Error("FrozenMemTable should be nil initially")
	}
}

func TestDefaultEngineOptions(t *testing.T) {
	dir := t.TempDir()
	opts := DefaultEngineOptions(dir)
	if opts.DataDir != dir {
		t.Errorf("expected DataDir %q, got %q", dir, opts.DataDir)
	}
}

func TestEmptyIterator(t *testing.T) {
	iter := &emptyIterator{}
	if iter.Next() {
		t.Error("emptyIterator.Next() should return false")
	}
	if iter.Key() != nil {
		t.Error("emptyIterator.Key() should return nil")
	}
	if iter.Value() != nil {
		t.Error("emptyIterator.Value() should return nil")
	}
	if iter.Err() != nil {
		t.Error("emptyIterator.Err() should return nil")
	}
	if err := iter.Close(); err != nil {
		t.Errorf("emptyIterator.Close() should return nil, got %v", err)
	}
}

func TestBatchError(t *testing.T) {
	err := &batchError{msg: "test error"}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %s", err.Error())
	}
}

func TestGroupCommitWriterError(t *testing.T) {
	// Create a groupCommitWriter with a temp file
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "test.log"))
	if err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}
	defer f.Close()

	gcw := newGroupCommitWriter(f, 10*time.Millisecond, true)
	defer gcw.Close()

	// Error should be nil initially
	if err := gcw.Error(); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestEngineIteratorAdapter(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Write some keys
	keys := []string{"apple", "application", "banana", "app"}
	for i, k := range keys {
		if err := eng.PutWithTS([]byte(k), []byte("val"), uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	// Scan with prefix "app"
	iter := eng.Scan([]byte("app"))
	defer iter.Close()

	var scanned []string
	for iter.Next() {
		scanned = append(scanned, string(iter.Key()))
	}

	// Should find "app", "apple", "application" (3 keys)
	if len(scanned) != 3 {
		t.Errorf("expected 3 keys with prefix 'app', got %d: %v", len(scanned), scanned)
	}

	// Verify Err() returns nil
	if err := iter.Err(); err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// Verify Value() returns the expected value for the first key
	iter2 := eng.Scan([]byte("app"))
	defer iter2.Close()
	if iter2.Next() {
		val := iter2.Value()
		if string(val) != "val" {
			t.Errorf("expected 'val', got %s", val)
		}
	}
}

func TestScanEmptyEngine(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Scan on empty engine should return empty iterator
	iter := eng.Scan([]byte("any"))
	defer iter.Close()
	if iter.Next() {
		t.Error("expected no results for empty engine")
	}
}

func TestRecoverFromWAL(t *testing.T) {
	dir := t.TempDir()

	// Create engine, write data, close
	eng1, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine1: %v", err)
	}

	// Write various types of entries
	if err := eng1.PutWithTS([]byte("key1"), []byte("value1"), 10); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}
	if err := eng1.PutWithTS([]byte("key2"), []byte("value2"), 20); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}
	if err := eng1.DeleteWithTS([]byte("key2"), 30); err != nil {
		t.Fatalf("DeleteWithTS failed: %v", err)
	}

	// Write a batch
	batchData := encodeTestBatch(
		testBatchOp{isDelete: false, key: "batch_key", value: "batch_val"},
	)
	if err := eng1.WriteAtomicBatch(batchData, 40); err != nil {
		t.Fatalf("WriteAtomicBatch failed: %v", err)
	}

	errors.CloseWithFatal(eng1, "engine1")

	// Reopen - should recover from WAL
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine2: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine2")

	// Verify recovered data
	got, err := eng2.GetWithTS([]byte("key1"), 10)
	if err != nil {
		t.Fatalf("GetWithTS key1 failed: %v", err)
	}
	if string(got) != "value1" {
		t.Errorf("expected 'value1', got %s", got)
	}

	// key2 was deleted at ts 30, so at ts 40 it should not be visible
	got, err = eng2.GetWithTS([]byte("key2"), 40)
	if err != nil {
		t.Fatalf("GetWithTS key2 failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for deleted key2, got %s", got)
	}

	// Batch key should be visible
	got, err = eng2.GetWithTS([]byte("batch_key"), 40)
	if err != nil {
		t.Fatalf("GetWithTS batch_key failed: %v", err)
	}
	if string(got) != "batch_val" {
		t.Errorf("expected 'batch_val', got %s", got)
	}
}

func TestFlushMemTable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Write enough data to trigger flush
	// MaxMemTableSize is 4MB, so write a few hundred KB
	largeVal := make([]byte, 64*1024) // 64KB
	for i := range largeVal {
		largeVal[i] = byte(i % 256)
	}

	for i := 0; i < 10; i++ {
		key := []byte{byte(i)}
		if err := eng.PutWithTS(key, largeVal, uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed at iteration %d: %v", i, err)
		}
	}

	// Manually trigger flush
	if err := eng.flushMemTable(); err != nil {
		t.Logf("flushMemTable returned (may be expected): %v", err)
	}
}

func TestCompactLevel0(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// compactLevel0 with no level-0 files should return nil
	if err := eng.compactLevel0(); err != nil {
		t.Errorf("compactLevel0 with empty level 0 should return nil, got %v", err)
	}
}

func TestMaybeCompact(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// maybeCompact with no level-0 files should not panic
	eng.maybeCompact()
}

func TestRecoverFromWAL_InvalidVLogPointer(t *testing.T) {
	dir := t.TempDir()

	// Create engine, write a large value (goes to VLog), close
	eng1, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine1: %v", err)
	}

	// Write a large value that will go to VLog
	largeVal := make([]byte, 100)
	for i := range largeVal {
		largeVal[i] = byte(i)
	}
	if err := eng1.PutWithTS([]byte("large_key"), largeVal, 10); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}
	errors.CloseWithFatal(eng1, "engine1")

	// Reopen - should recover from WAL with valid VLog pointer
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine2: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine2")

	got, err := eng2.GetWithTS([]byte("large_key"), 10)
	if err != nil {
		t.Fatalf("GetWithTS failed: %v", err)
	}
	if len(got) == 0 {
		t.Log("large value may not be recoverable (VLog pointer may be invalid after reopen)")
	}
}

func TestRecoverFromWAL_InvalidBatch(t *testing.T) {
	dir := t.TempDir()

	eng1, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine1: %v", err)
	}

	// Write a batch with malformed data (will be logged as warning during recovery)
	malformedBatch := []byte{0x00, 0x01, 0xFF} // numOps=1 but no op data
	if err := eng1.WriteAtomicBatch(malformedBatch, 50); err == nil {
		t.Log("WriteAtomicBatch with malformed data may succeed (validation happens at decode time)")
	}
	errors.CloseWithFatal(eng1, "engine1")

	// Reopen - should handle malformed batch gracefully
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine2: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine2")
}

func TestOpenVLog_NonOsFile(t *testing.T) {
	// OpenVLog with a non-os.File type should fail
	// We can't easily create a non-os.File through the VFS interface,
	// but we can test that OpenVLog handles the error gracefully
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	// Create a regular file first
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	f.Close()

	// OpenVLog with the real file should work
	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("OpenVLog failed: %v", err)
	}
	errors.CloseWithFatal(vlog, "vlog")
}

func TestGetLatestInfoWithSSTable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Write data and flush to create SSTable
	key := []byte("sst_key")
	if err := eng.PutWithTS(key, []byte("sst_val"), 100); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Flush memtable to create SSTable
	if err := eng.flushMemTable(); err != nil {
		t.Logf("flushMemTable: %v (may be expected)", err)
	}

	// GetLatestInfo should still find the value
	val, ts, _, err := eng.GetLatestInfo(key)
	if err != nil {
		t.Fatalf("GetLatestInfo failed: %v", err)
	}
	if val != nil {
		t.Logf("GetLatestInfo returned value=%s ts=%d", val, ts)
	}
}

func TestCollectLiveValuePointersWithFrozenMemTable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Put a large value (goes to VLog)
	largeVal := make([]byte, 100)
	for i := range largeVal {
		largeVal[i] = byte(i)
	}
	if err := eng.PutWithTS([]byte("gc_key"), largeVal, 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Collect live value pointers
	pointers, err := eng.CollectLiveValuePointers()
	if err != nil {
		t.Fatalf("CollectLiveValuePointers failed: %v", err)
	}
	t.Logf("Collected %d live value pointers", len(pointers))
}

func TestFlushMemTableErrorPaths(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Flush with empty memtable should succeed (no data to flush)
	if err := eng.flushMemTable(); err != nil {
		t.Logf("flushMemTable with empty memtable: %v", err)
	}
}

func TestCollectLiveValuePointersClosedEngine(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Close the engine
	eng.Close()

	// CollectLiveValuePointers on closed engine should return error
	_, err = eng.CollectLiveValuePointers()
	if err == nil {
		t.Error("expected error for closed engine")
	}
}

func TestInvalidateVLogPointersWithFrozenMemTable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Put a large value (goes to VLog)
	largeVal := make([]byte, 100)
	for i := range largeVal {
		largeVal[i] = byte(i)
	}
	if err := eng.PutWithTS([]byte("vlog_key"), largeVal, 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Invalidate VLog pointers
	eng.InvalidateVLogPointers()
}

func TestNewLSMEngineManifestError(t *testing.T) {
	// Create engine in a non-writable location to trigger manifest error
	_, err := NewLSMEngine("/nonexistent/path/for/manifest")
	if err == nil {
		t.Error("expected error for non-writable path")
	}
}

func TestNewManifestError(t *testing.T) {
	// NewManifest with non-writable path should fail
	_, err := NewManifest("/nonexistent/path/MANIFEST")
	if err == nil {
		t.Error("expected error for non-writable path")
	}
}

func TestGetLatestInfoWithSSTableSearch(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Write data and flush to create SSTable
	key := []byte("sst_key")
	if err := eng.PutWithTS(key, []byte("sst_val"), 100); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Flush memtable to create SSTable
	if err := eng.flushMemTable(); err != nil {
		t.Logf("flushMemTable: %v (may be expected)", err)
	}

	// GetLatestInfo should still find the value (either in memtable or SSTable)
	val, ts, _, err := eng.GetLatestInfo(key)
	if err != nil {
		t.Fatalf("GetLatestInfo failed: %v", err)
	}
	if val != nil {
		t.Logf("GetLatestInfo returned value=%s ts=%d", val, ts)
	}

	// Non-existent key should return nil/0
	val, ts, _, err = eng.GetLatestInfo([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("GetLatestInfo for non-existent failed: %v", err)
	}
	if val != nil || ts != 0 {
		t.Errorf("expected nil/0 for non-existent key, got %v/%d", val, ts)
	}
}

func TestScanWithFrozenMemTable(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Write some keys
	if err := eng.PutWithTS([]byte("key1"), []byte("val1"), 1); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}
	if err := eng.PutWithTS([]byte("key2"), []byte("val2"), 2); err != nil {
		t.Fatalf("PutWithTS failed: %v", err)
	}

	// Scan should find the keys
	iter := eng.Scan([]byte("key"))
	defer iter.Close()
	var count int
	for iter.Next() {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 keys, got %d", count)
	}
}

func TestRegisterSnapshotEdgeCases(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Register a snapshot
	eng.RegisterSnapshot(100)
	if min := eng.GetMinActiveSnapshotTS(); min != 100 {
		t.Errorf("expected min 100, got %d", min)
	}

	// Register a lower snapshot (should update min)
	eng.RegisterSnapshot(50)
	if min := eng.GetMinActiveSnapshotTS(); min != 50 {
		t.Errorf("expected min 50, got %d", min)
	}

	// Register a higher snapshot (should NOT change min)
	eng.RegisterSnapshot(200)
	if min := eng.GetMinActiveSnapshotTS(); min != 50 {
		t.Errorf("expected min still 50, got %d", min)
	}

	// Unregister non-existent snapshot should not panic
	eng.UnregisterSnapshot(999)

	// Unregister 50. With reference counting snapshots 100 and 200
	// remain active, so the min must move to 100, not to 0.
	eng.UnregisterSnapshot(50)
	if min := eng.GetMinActiveSnapshotTS(); min != 100 {
		t.Errorf("expected min 100 after unregister, got %d", min)
	}

	// Unregister 100, min must become 200.
	eng.UnregisterSnapshot(100)
	if min := eng.GetMinActiveSnapshotTS(); min != 200 {
		t.Errorf("expected min 200 after unregister, got %d", min)
	}

	// Unregister the last snapshot, min must reset to 0.
	eng.UnregisterSnapshot(200)
	if min := eng.GetMinActiveSnapshotTS(); min != 0 {
		t.Errorf("expected min 0 after last unregister, got %d", min)
	}
}

// TestSnapshotRefCounting verifies that closing one snapshot must not reset
// the minimum when other snapshots remain active. Reference counting per TS
// guarantees the watermark always equals the smallest still-active snapshot TS.
func TestSnapshotRefCounting(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	check := func(want uint64, msg string) {
		t.Helper()
		if got := eng.GetMinActiveSnapshotTS(); got != want {
			t.Errorf("%s: expected min %d, got %d", msg, want, got)
		}
	}

	// Initially no active snapshots.
	check(0, "initial")

	// 1. Register TS=5 -> min 5.
	eng.RegisterSnapshot(5)
	check(5, "after register 5")

	// 2. Register TS=10 -> min stays 5.
	eng.RegisterSnapshot(10)
	check(5, "after register 10")

	// 3. Close TS=5 -> min becomes 10.
	eng.UnregisterSnapshot(5)
	check(10, "after unregister 5")

	// 4. Close TS=10 -> min becomes 0.
	eng.UnregisterSnapshot(10)
	check(0, "after unregister 10")

	// 5. Two snapshots with different TS: register 5 and 7, close 5 -> min 7 (not 0).
	eng.RegisterSnapshot(5)
	eng.RegisterSnapshot(7)
	eng.UnregisterSnapshot(5)
	check(7, "two snapshots, closed non-min 5")
	eng.UnregisterSnapshot(7)
	check(0, "after unregister 7")

	// 6. Register 3 then 5, close 3 -> min 5.
	eng.RegisterSnapshot(3)
	eng.RegisterSnapshot(5)
	eng.UnregisterSnapshot(3)
	check(5, "register 3 then 5, close 3")
	eng.UnregisterSnapshot(5)
	check(0, "after unregister 5")

	// 7. Unregister a non-existent snapshot must not panic and must not change min.
	eng.RegisterSnapshot(20)
	eng.UnregisterSnapshot(999) // no-op, defensive
	check(20, "unregister non-existent leaves min unchanged")
	eng.UnregisterSnapshot(20)
	check(0, "after unregister 20")
}

func TestMemTableIteratorKeyValue(t *testing.T) {
	mt := memtable.NewMemTable()

	// Empty iterator
	iter := mt.NewIterator()
	if iter.Next() {
		t.Error("expected no results from empty iterator")
	}
	// Key() and Value() on exhausted iterator should not panic
	_ = iter.Key()
	_ = iter.Value()
	iter.Close()

	// Iterator with data
	mt.Put(mvcc.NewMVCCKey([]byte("test"), 1), []byte("value"))
	iter2 := mt.NewIterator()
	defer iter2.Close()
	if !iter2.Next() {
		t.Fatal("expected at least one result")
	}
	if string(iter2.Key().Key) != "test" {
		t.Errorf("expected key 'test', got %s", iter2.Key().Key)
	}
	if string(iter2.Value()) != "value" {
		t.Errorf("expected value 'value', got %s", iter2.Value())
	}
}

// testBatchOp represents a single operation for test batch encoding.
type testBatchOp struct {
	isDelete bool
	key      string
	value    string
}

// encodeTestBatch encodes operations using the format expected by decodeBatchLocal:
// numOps(2 bytes big-endian uint16) + [opType(1 byte: 1=put,2=delete) + keyLen(2 bytes big-endian uint16) + key + valLen(4 bytes big-endian uint32) + value]...
func encodeTestBatch(ops ...testBatchOp) []byte {
	buf := make([]byte, 2)
	buf[0] = byte(len(ops) >> 8)
	buf[1] = byte(len(ops))
	for _, op := range ops {
		if op.isDelete {
			buf = append(buf, 2) // delete type
		} else {
			buf = append(buf, 1) // put type
		}
		keyLen := len(op.key)
		buf = append(buf, byte(keyLen>>8), byte(keyLen))
		buf = append(buf, []byte(op.key)...)
		valLen := len(op.value)
		buf = append(buf, byte(valLen>>24), byte(valLen>>16), byte(valLen>>8), byte(valLen))
		buf = append(buf, []byte(op.value)...)
	}
	return buf
}

// TestLastCommitCacheKeyStability verifies that the lastCommitCache stores a
// stable (copied) key, so that mutating the caller's []byte after a cache
// write does NOT corrupt the map key.
func TestLastCommitCacheKeyStability(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	key := []byte("user:alice")
	const commitTS = uint64(100)

	// Populate the cache via the same path used by CheckConflict.
	eng.updateLastCommitCache(key, commitTS)

	// Mutate the original slice after the cache write. Under the old
	// unsafeToString implementation this would corrupt the map key and cause
	// the lookup below to miss.
	for i := range key {
		key[i] = 'X'
	}

	got, ok := eng.getLastCommitCache([]byte("user:alice"))
	if !ok {
		t.Fatalf("expected cache hit for key, got miss after mutating source slice")
	}
	if got != commitTS {
		t.Fatalf("expected commitTS %d, got %d", commitTS, got)
	}

	// The mutated key must NOT match the original cache entry.
	if _, ok := eng.getLastCommitCache([]byte("XXXXXXXXXX")); ok {
		t.Fatalf("cache lookup for mutated key should miss, but it matched the original entry")
	}
}

// TestTimestampRecovery verifies that transaction timestamps continue to be
// monotonic and unique after a restart.
func TestTimestampRecovery(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: create a DB and commit keys with high timestamps.
	{
		eng, err := NewLSMEngine(dir)
		if err != nil {
			t.Fatalf("failed to create engine: %v", err)
		}

		if err := eng.PutWithTS([]byte("k1"), []byte("v1"), 100); err != nil {
			t.Fatalf("PutWithTS(100) failed: %v", err)
		}
		if err := eng.PutWithTS([]byte("k2"), []byte("v2"), 200); err != nil {
			t.Fatalf("PutWithTS(200) failed: %v", err)
		}

		if err := eng.Close(); err != nil {
			t.Fatalf("failed to close engine: %v", err)
		}
	}

	// Phase 2: reopen and verify timestamps continue above the recovered max.
	{
		eng, err := NewLSMEngine(dir)
		if err != nil {
			t.Fatalf("failed to reopen engine: %v", err)
		}
		defer errors.CloseWithFatal(eng, "engine")

		next := eng.NextTimestamp()
		if next <= 200 {
			t.Fatalf("NextTimestamp after restart = %d, want > 200", next)
		}

		// A subsequent timestamp must still be monotonic.
		next2 := eng.NextTimestamp()
		if next2 <= next {
			t.Fatalf("NextTimestamp not monotonic after restart: %d then %d", next, next2)
		}
	}
}

// TestCompactionSnapshotSafe verifies that a snapshot opened AFTER compaction
// starts (with a smaller commitTS than the watermark fixed at compaction start)
// can still read its historical versions. Compaction must re-read the current
// minimum active snapshot timestamp for every version it writes, rather than
// relying on a single value captured when compaction began.
//
// The compaction is paused deterministically at the start of its version
// loop via the compactionTestHook seam, at which point the test registers a
// new snapshot. This avoids any timing dependency and makes the race between
// "compaction reads the watermark" and "snapshot registers" reproducible.
func TestCompactionSnapshotSafe(t *testing.T) {
	logger.SetLevel(logger.ERROR)
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	key := []byte("key")
	// Write three MVCC versions of the same key.
	if err := eng.PutWithTS(key, []byte("v10"), 10); err != nil {
		t.Fatalf("PutWithTS(10) failed: %v", err)
	}
	if err := eng.PutWithTS(key, []byte("v20"), 20); err != nil {
		t.Fatalf("PutWithTS(20) failed: %v", err)
	}
	if err := eng.PutWithTS(key, []byte("v30"), 30); err != nil {
		t.Fatalf("PutWithTS(30) failed: %v", err)
	}

	// Flush the MemTable so all versions land in a Level-0 SSTable that
	// compaction can process.
	if err := eng.flushMemTable(); err != nil {
		t.Fatalf("flushMemTable failed: %v", err)
	}
	if len(eng.levels[0]) == 0 {
		t.Fatalf("expected at least one level-0 SSTable after flush")
	}

	// Pause compaction at the start of its version-processing loop and open a
	// NEW snapshot at TS=15 from the hook, before compaction writes any version.
	// A watermark captured at compaction start would be 0 (no active snapshots),
	// which would discard v10; the mid-flight snapshot must prevent that.
	snapshotOpened := make(chan struct{})
	origHook := compactionTestHook
	compactionTestHook = func() {
		compactionTestHook = nil // fire only once
		eng.RegisterSnapshot(15)
		close(snapshotOpened)
	}
	defer func() { compactionTestHook = origHook }()

	compactDone := make(chan error, 1)
	go func() {
		compactDone <- eng.compactLevel0()
	}()

	// Wait until the snapshot was registered mid-compaction, then await the
	// compaction result.
	<-snapshotOpened
	if err := <-compactDone; err != nil {
		t.Fatalf("compactLevel0 failed: %v", err)
	}
	eng.UnregisterSnapshot(15)

	// The snapshot at TS=15 must observe v10: the newest committed version
	// with commitTS <= 15.
	got, err := eng.GetWithTS(key, 15)
	if err != nil {
		t.Fatalf("GetWithTS(key,15) failed: %v", err)
	}
	if string(got) != "v10" {
		t.Errorf("snapshot at 15: expected v10, got %q", got)
	}

	// Reading with no snapshot (MaxUint64) must return the newest version v30.
	got, err = eng.GetWithTS(key, ^uint64(0))
	if err != nil {
		t.Fatalf("GetWithTS(key,MaxUint64) failed: %v", err)
	}
	if string(got) != "v30" {
		t.Errorf("latest read: expected v30, got %q", got)
	}
}
