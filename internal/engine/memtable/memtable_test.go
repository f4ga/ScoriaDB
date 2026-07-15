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

package memtable

import (
	"sync"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

func TestMemTablePutGet(t *testing.T) {
	mt := NewMemTable()

	key := []byte("test_key")
	value := []byte("test_value")
	ts := uint64(100)

	mvccKey := mvcc.NewMVCCKey(key, ts)
	mt.Put(mvccKey, value)

	got, found := mt.Get(mvccKey)
	if !found {
		t.Fatal("key not found")
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}
}

func TestMemTableMultipleVersions(t *testing.T) {
	mt := NewMemTable()

	key := []byte("key")
	v1 := []byte("value1")
	v2 := []byte("value2")
	v3 := []byte("value3")

	// Вставляем версии с разными timestamp
	mt.Put(mvcc.NewMVCCKey(key, 10), v1)
	mt.Put(mvcc.NewMVCCKey(key, 20), v2)
	mt.Put(mvcc.NewMVCCKey(key, 30), v3)

	// Запрос с snapshotTS = 25 должен вернуть v2 (последняя версия <= 25)
	k25 := mvcc.NewMVCCKey(key, 25)
	got, found := mt.Get(k25)
	if !found {
		t.Fatal("key not found for ts 25")
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}

	// Запрос с snapshotTS = 30 должен вернуть v3
	k30 := mvcc.NewMVCCKey(key, 30)
	got, found = mt.Get(k30)
	if !found {
		t.Fatal("key not found for ts 30")
	}
	if string(got) != string(v3) {
		t.Errorf("expected %s, got %s", v3, got)
	}

	// Запрос с snapshotTS = 5 должен не найти (версии только с 10)
	k5 := mvcc.NewMVCCKey(key, 5)
	_, found = mt.Get(k5)
	if found {
		t.Error("expected not found for ts 5")
	}
}

func TestMemTableIterator(t *testing.T) {
	mt := NewMemTable()

	// Вставляем несколько ключей
	mt.Put(mvcc.NewMVCCKey([]byte("a"), 10), []byte("val_a"))
	mt.Put(mvcc.NewMVCCKey([]byte("b"), 20), []byte("val_b"))
	mt.Put(mvcc.NewMVCCKey([]byte("c"), 30), []byte("val_c"))

	iter := mt.NewIterator()
	defer iter.Close()

	var keys []string
	for iter.Next() {
		keys = append(keys, string(iter.Key().Key))
	}

	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
	// Порядок должен быть сортирован по ключу и timestamp
	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if keys[i] != exp {
			t.Errorf("key %d: expected %s, got %s", i, exp, keys[i])
		}
	}
}

func TestMemTableSize(t *testing.T) {
	mt := NewMemTable()
	if mt.Size() != 0 {
		t.Errorf("initial size should be 0, got %d", mt.Size())
	}

	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 1), []byte("v1"))
	if mt.Size() != 1 {
		t.Errorf("size after one insert should be 1, got %d", mt.Size())
	}

	// Обновление того же ключа с тем же timestamp не должно увеличивать размер
	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 1), []byte("v2"))
	if mt.Size() != 1 {
		t.Errorf("size after update should still be 1, got %d", mt.Size())
	}

	// Новая версия того же ключа увеличивает размер? В нашей реализации - да, потому что это другая запись
	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 2), []byte("v3"))
	if mt.Size() != 2 {
		t.Errorf("size after new version should be 2, got %d", mt.Size())
	}
}

// TestMVCCGetWithMultipleVersions проверяет корректность поиска видимой версии
// с учётом tombstone и различных snapshotTS.
func TestMVCCGetWithMultipleVersions(t *testing.T) {
	mt := NewMemTable()
	key := []byte("user1")

	// Вставляем три версии с разными commitTS
	v1 := []byte("value1")
	v2 := []byte("value2")
	v3 := []byte("value3")
	mt.Put(mvcc.NewMVCCKey(key, 10), v1)
	mt.Put(mvcc.NewMVCCKey(key, 20), v2)
	mt.Put(mvcc.NewMVCCKey(key, 30), v3)

	// snapshotTS=15 должен вернуть v1 (commitTS 10)
	got, found := mt.Get(mvcc.NewMVCCKey(key, 15))
	if !found {
		t.Fatal("key not found for snapshotTS=15")
	}
	if string(got) != string(v1) {
		t.Errorf("expected %s, got %s", v1, got)
	}

	// snapshotTS=25 должен вернуть v2 (commitTS 20)
	got, found = mt.Get(mvcc.NewMVCCKey(key, 25))
	if !found {
		t.Fatal("key not found for snapshotTS=25")
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}

	// snapshotTS=5 должен вернуть ErrKeyNotFound (все версии новее)
	_, found = mt.Get(mvcc.NewMVCCKey(key, 5))
	if found {
		t.Error("expected not found for snapshotTS=5")
	}

	// Проверка tombstone: удаляем ключ с commitTS=20 (используем DeleteWithTS)
	mt.DeleteWithTS(mvcc.NewMVCCKey(key, 20)) // tombstone
	// snapshotTS=25 должен вернуть ErrKeyNotFound (tombstone скрывает ключ)
	_, found = mt.Get(mvcc.NewMVCCKey(key, 25))
	if found {
		t.Error("expected not found for snapshotTS=25 after tombstone")
	}
	// snapshotTS=15 должен вернуть v1 (commitTS 10), потому что tombstone ещё не виден
	got, found = mt.Get(mvcc.NewMVCCKey(key, 15))
	if !found {
		t.Fatal("key not found for snapshotTS=15 after tombstone")
	}
	if string(got) != string(v1) {
		t.Errorf("expected %s, got %s", v1, got)
	}
}

func TestMemTableConcurrentPutGet(t *testing.T) {
	mt := NewMemTable()
	var wg sync.WaitGroup
	numWorkers := 10
	numOps := 100

	// Concurrent writes
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := mvcc.NewMVCCKey([]byte("key"), uint64(id*numOps+j))
				mt.Put(key, []byte("value"))
			}
		}(i)
	}
	wg.Wait()

	// Verify all keys are readable
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < numOps; j++ {
			key := mvcc.NewMVCCKey([]byte("key"), uint64(i*numOps+j))
			val, found := mt.Get(key)
			if !found {
				t.Errorf("key not found: %v", key)
				continue
			}
			if string(val) != "value" {
				t.Errorf("wrong value for key %v: expected 'value', got '%s'", key, string(val))
			}
		}
	}
}

func TestMemTableGetWithSnapshot(t *testing.T) {
	mt := NewMemTable()

	// Insert three versions of the same key
	mt.Put(mvcc.NewMVCCKey([]byte("foo"), 1), []byte("v1"))
	mt.Put(mvcc.NewMVCCKey([]byte("foo"), 2), []byte("v2"))
	mt.Put(mvcc.NewMVCCKey([]byte("foo"), 3), []byte("v3"))

	// Read with different snapshotTS
	tests := []struct {
		snapshotTS uint64
		expected   string
		found      bool
	}{
		{3, "v3", true}, // newest
		{2, "v2", true}, // middle
		{1, "v1", true}, // oldest
		{0, "", false},  // no version
		{4, "v3", true}, // greater than newest → newest
	}

	for _, tt := range tests {
		key := mvcc.NewMVCCKey([]byte("foo"), tt.snapshotTS)
		val, found := mt.Get(key)
		if found != tt.found {
			t.Errorf("snapshot=%d: found=%v, expected=%v", tt.snapshotTS, found, tt.found)
			continue
		}
		if found && string(val) != tt.expected {
			t.Errorf("snapshot=%d: value='%s', expected='%s'", tt.snapshotTS, string(val), tt.expected)
		}
	}
}

// ============================================================
// Arena tests
// ============================================================

func TestArenaNewArena(t *testing.T) {
	a := NewArena()
	if a == nil {
		t.Fatal("NewArena() returned nil")
	}
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block, got %d", a.NumBlocks())
	}
	if a.Size() != 0 {
		t.Errorf("expected Size()=0 for new arena, got %d", a.Size())
	}
}

func TestArenaAllocAndSize(t *testing.T) {
	a := NewArena()

	p1 := a.Alloc(8)
	if p1 == nil {
		t.Fatal("Alloc(8) returned nil")
	}
	size1 := a.Size()
	if size1 < 8 {
		t.Errorf("expected Size() >= 8, got %d", size1)
	}

	p2 := a.Alloc(16)
	if p2 == nil {
		t.Fatal("Alloc(16) returned nil")
	}
	size2 := a.Size()
	if size2 < size1+16 {
		t.Errorf("expected Size() >= %d, got %d", size1+16, size2)
	}

	// Allocate zero bytes
	p3 := a.Alloc(0)
	if p3 == nil {
		t.Fatal("Alloc(0) returned nil")
	}
}

func TestArenaReset(t *testing.T) {
	a := NewArena()

	// Allocate some nodes
	p1 := a.Alloc(64)
	p2 := a.Alloc(128)
	_ = p1
	_ = p2

	if a.Size() == 0 {
		t.Error("expected Size() > 0 after allocations")
	}
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block, got %d", a.NumBlocks())
	}

	// Reset
	a.Reset()

	if a.Size() != 0 {
		t.Errorf("expected Size()=0 after Reset, got %d", a.Size())
	}
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block after Reset, got %d", a.NumBlocks())
	}

	// After Reset, new allocations should work
	p3 := a.Alloc(32)
	if p3 == nil {
		t.Fatal("Alloc after Reset returned nil")
	}
	if a.Size() < 32 {
		t.Errorf("expected Size() >= 32 after new alloc, got %d", a.Size())
	}
}

func TestArenaResetEmpty(t *testing.T) {
	a := NewArena()
	// Reset on empty arena should not panic
	a.Reset()
	if a.Size() != 0 {
		t.Errorf("expected Size()=0 after Reset on empty, got %d", a.Size())
	}
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block after Reset on empty, got %d", a.NumBlocks())
	}
}

func TestArenaMultipleBlocks(t *testing.T) {
	a := NewArena()

	// Allocate enough to exceed one block (64 MB)
	blockSize := ArenaBlockSize
	numAllocs := 3
	for i := 0; i < numAllocs; i++ {
		// Allocate half a block each time
		_ = a.Alloc(blockSize / 2)
	}

	if a.NumBlocks() < 2 {
		t.Errorf("expected at least 2 blocks after large allocations, got %d", a.NumBlocks())
	}

	totalSize := a.Size()
	if totalSize == 0 {
		t.Error("expected Size() > 0 after large allocations")
	}
}

func TestArenaNewNode(t *testing.T) {
	a := NewArena()

	key := []byte("test_key")
	value := []byte("test_value")
	node := a.NewNode(key, value, 10)

	if node == nil {
		t.Fatal("NewNode returned nil")
	}

	// Check key
	nodeKey := node.Key()
	if string(nodeKey.Key) != string(key) {
		t.Errorf("expected key %s, got %s", key, nodeKey.Key)
	}

	// Check value
	nodeVal := node.Value()
	if string(nodeVal) != string(value) {
		t.Errorf("expected value %s, got %s", value, nodeVal)
	}

	// Check height
	if node.height != 10 {
		t.Errorf("expected height 10, got %d", node.height)
	}

	// Check deleted flag
	if node.deleted.Load() {
		t.Error("expected deleted=false for new node")
	}
}

func TestArenaNewNodeNilKeyValue(t *testing.T) {
	a := NewArena()

	node := a.NewNode(nil, nil, 1)
	if node == nil {
		t.Fatal("NewNode with nil key/value returned nil")
	}

	nodeKey := node.Key()
	if nodeKey.Key != nil {
		t.Errorf("expected nil key, got %v", nodeKey.Key)
	}

	nodeVal := node.Value()
	if nodeVal != nil {
		t.Errorf("expected nil value, got %v", nodeVal)
	}
}

func TestArenaSizeAfterResetAndRealloc(t *testing.T) {
	a := NewArena()

	// Allocate, reset, allocate again
	_ = a.Alloc(100)
	a.Reset()

	if a.Size() != 0 {
		t.Errorf("expected Size()=0 after Reset, got %d", a.Size())
	}

	// Allocate again
	_ = a.Alloc(200)
	if a.Size() < 200 {
		t.Errorf("expected Size() >= 200 after realloc, got %d", a.Size())
	}
}

func TestArenaNumBlocks(t *testing.T) {
	a := NewArena()

	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block initially, got %d", a.NumBlocks())
	}

	// Force growth by allocating large chunks
	halfBlock := ArenaBlockSize / 2
	_ = a.Alloc(halfBlock)
	_ = a.Alloc(halfBlock) // fills the first block exactly (64MB)
	_ = a.Alloc(1)         // third allocation triggers growth

	if a.NumBlocks() < 2 {
		t.Errorf("expected >= 2 blocks after growth, got %d", a.NumBlocks())
	}

	// Reset should bring back to 1 block
	a.Reset()
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block after Reset, got %d", a.NumBlocks())
	}
}

func TestArenaConcurrentAlloc(t *testing.T) {
	a := NewArena()
	var wg sync.WaitGroup
	numGoroutines := 10
	allocsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < allocsPerGoroutine; j++ {
				ptr := a.Alloc(8)
				if ptr == nil {
					t.Error("Alloc returned nil in concurrent test")
					return
				}
			}
		}()
	}
	wg.Wait()

	totalSize := a.Size()
	expectedMin := uint64(numGoroutines * allocsPerGoroutine * 8)
	if totalSize < expectedMin {
		t.Errorf("expected Size() >= %d, got %d", expectedMin, totalSize)
	}
}

// ============================================================
// MemTable additional tests
// ============================================================

func TestMemTableGetLatest(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	// Test on empty table
	val, ts, found := mt.GetLatest(key)
	if found {
		t.Error("expected not found on empty table")
	}
	if val != nil {
		t.Errorf("expected nil value, got %v", val)
	}
	if ts != 0 {
		t.Errorf("expected ts=0, got %d", ts)
	}

	// Insert one version
	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value1"))
	val, ts, found = mt.GetLatest(key)
	if !found {
		t.Fatal("expected found after insert")
	}
	if string(val) != "value1" {
		t.Errorf("expected 'value1', got '%s'", val)
	}
	if ts != 100 {
		t.Errorf("expected ts=100, got %d", ts)
	}

	// Insert newer version
	mt.Put(mvcc.NewMVCCKey(key, 200), []byte("value2"))
	val, ts, found = mt.GetLatest(key)
	if !found {
		t.Fatal("expected found after second insert")
	}
	if string(val) != "value2" {
		t.Errorf("expected 'value2', got '%s'", val)
	}
	if ts != 200 {
		t.Errorf("expected ts=200, got %d", ts)
	}

	// Insert older version (should not change latest)
	mt.Put(mvcc.NewMVCCKey(key, 50), []byte("value0"))
	val, ts, found = mt.GetLatest(key)
	if !found {
		t.Fatal("expected found after older insert")
	}
	if string(val) != "value2" {
		t.Errorf("expected 'value2', got '%s'", val)
	}
	if ts != 200 {
		t.Errorf("expected ts=200, got %d", ts)
	}
}

func TestMemTableGetLatestTombstone(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	// Insert a version, then delete it
	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value1"))
	mt.DeleteWithTS(mvcc.NewMVCCKey(key, 100))

	// GetLatest should skip the tombstone and return not found
	val, ts, found := mt.GetLatest(key)
	if found {
		t.Error("expected not found after tombstone")
	}
	if val != nil {
		t.Errorf("expected nil value, got %v", val)
	}
	if ts != 0 {
		t.Errorf("expected ts=0, got %d", ts)
	}
}

func TestMemTableGetLatestMultipleVersionsWithTombstone(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	// Insert versions, then delete the latest
	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value1"))
	mt.Put(mvcc.NewMVCCKey(key, 200), []byte("value2"))
	mt.DeleteWithTS(mvcc.NewMVCCKey(key, 200))

	// GetLatest should return the non-deleted version (value1, ts=100)
	val, ts, found := mt.GetLatest(key)
	if !found {
		t.Fatal("expected found, older version should be visible")
	}
	if string(val) != "value1" {
		t.Errorf("expected 'value1', got '%s'", val)
	}
	if ts != 100 {
		t.Errorf("expected ts=100, got %d", ts)
	}
}

func TestMemTableLastKey(t *testing.T) {
	mt := NewMemTable()

	// Empty table
	lastKey := mt.LastKey()
	if lastKey.Key != nil {
		t.Errorf("expected nil key for empty table, got %v", lastKey.Key)
	}

	// Insert keys in order
	mt.Put(mvcc.NewMVCCKey([]byte("a"), 100), []byte("val_a"))
	mt.Put(mvcc.NewMVCCKey([]byte("b"), 100), []byte("val_b"))
	mt.Put(mvcc.NewMVCCKey([]byte("c"), 100), []byte("val_c"))

	lastKey = mt.LastKey()
	if string(lastKey.Key) != "c" {
		t.Errorf("expected last key 'c', got '%s'", lastKey.Key)
	}

	// Insert keys out of order
	mt2 := NewMemTable()
	mt2.Put(mvcc.NewMVCCKey([]byte("z"), 100), []byte("val_z"))
	mt2.Put(mvcc.NewMVCCKey([]byte("a"), 100), []byte("val_a"))
	mt2.Put(mvcc.NewMVCCKey([]byte("m"), 100), []byte("val_m"))

	lastKey = mt2.LastKey()
	if string(lastKey.Key) != "z" {
		t.Errorf("expected last key 'z', got '%s'", lastKey.Key)
	}
}

func TestMemTableClose(t *testing.T) {
	mt := NewMemTable()

	// Insert data
	mt.Put(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value"))

	// Close
	mt.Close()

	// After Close, Get should return false without panic
	val, found := mt.Get(mvcc.NewMVCCKey([]byte("key"), 100))
	if found {
		t.Error("expected not found after Close")
	}
	if val != nil {
		t.Errorf("expected nil value after Close, got %v", val)
	}

	// GetLatest should return not found
	_, _, found = mt.GetLatest([]byte("key"))
	if found {
		t.Error("expected not found from GetLatest after Close")
	}

	// LastKey should return empty key
	lastKey := mt.LastKey()
	if lastKey.Key != nil {
		t.Errorf("expected nil key after Close, got %v", lastKey.Key)
	}

	// Size should be 0
	if mt.Size() != 0 {
		t.Errorf("expected Size()=0 after Close, got %d", mt.Size())
	}
}

func TestMemTableCloseEmpty(t *testing.T) {
	mt := NewMemTable()
	// Close on empty table should not panic
	mt.Close()
}

func TestMemTableCloseTwice(t *testing.T) {
	mt := NewMemTable()
	mt.Close()
	// Second Close should not panic
	mt.Close()
}

func TestMemTableDeleteWithTS(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	// Write at commitTS=100
	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value"))

	// Delete (tombstone) at commitTS=200
	mt.DeleteWithTS(mvcc.NewMVCCKey(key, 200))

	// SNAPSHOT BEFORE WRITE — NOT FOUND
	_, found := mt.Get(mvcc.NewMVCCKey(key, 50))
	if found {
		t.Error("expected NOT found at snapshot 50 (before write)")
	}

	// SNAPSHOT BETWEEN WRITE AND DELETE — FOUND
	val, found := mt.Get(mvcc.NewMVCCKey(key, 150))
	if !found {
		t.Fatal("expected found at snapshot 150")
	}
	if string(val) != "value" {
		t.Errorf("expected 'value', got '%s'", val)
	}

	// SNAPSHOT AFTER DELETE — NOT FOUND
	_, found = mt.Get(mvcc.NewMVCCKey(key, 250))
	if found {
		t.Error("expected NOT found at snapshot 250 (after delete)")
	}
}

func TestMemTableGetNonExistent(t *testing.T) {
	mt := NewMemTable()

	_, found := mt.Get(mvcc.NewMVCCKey([]byte("nonexistent"), 100))
	if found {
		t.Error("expected not found for non-existent key")
	}
}

func TestMemTableIteratorDeleted(t *testing.T) {
	mt := NewMemTable()

	mt.Put(mvcc.NewMVCCKey([]byte("a"), 100), []byte("val_a"))
	mt.Put(mvcc.NewMVCCKey([]byte("b"), 100), []byte("val_b"))
	mt.Put(mvcc.NewMVCCKey([]byte("c"), 100), []byte("val_c"))

	// Delete key "b"
	mt.DeleteWithTS(mvcc.NewMVCCKey([]byte("b"), 100))

	// Iterate — should skip deleted entries
	iter := mt.NewIterator()
	defer iter.Close()

	var keys []string
	for iter.Next() {
		keys = append(keys, string(iter.Key().Key))
	}

	if len(keys) != 2 {
		t.Errorf("expected 2 keys after delete, got %d: %v", len(keys), keys)
	}
	if len(keys) >= 2 {
		if keys[0] != "a" || keys[1] != "c" {
			t.Errorf("expected [a c], got %v", keys)
		}
	}
}

func TestMemTableIteratorEmpty(t *testing.T) {
	mt := NewMemTable()

	iter := mt.NewIterator()
	defer iter.Close()

	if iter.Next() {
		t.Error("expected no items from empty iterator")
	}
}

func TestMemTableConcurrentReadWrite(t *testing.T) {
	mt := NewMemTable()
	var wg sync.WaitGroup

	// Writer goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := mvcc.NewMVCCKey([]byte("shared_key"), uint64(id*50+j))
				mt.Put(key, []byte("value"))
			}
		}(i)
	}

	// Reader goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				// Read with various timestamps
				mt.Get(mvcc.NewMVCCKey([]byte("shared_key"), uint64(j)))
			}
		}()
	}

	wg.Wait()
}
