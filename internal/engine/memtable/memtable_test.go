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
	"fmt"
	"sync"
	"testing"
	"time"

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

	mt.Put(mvcc.NewMVCCKey(key, 10), v1)
	mt.Put(mvcc.NewMVCCKey(key, 20), v2)
	mt.Put(mvcc.NewMVCCKey(key, 30), v3)

	// snapshotTS = 25 → v2
	k25 := mvcc.NewMVCCKey(key, 25)
	got, found := mt.Get(k25)
	if !found {
		t.Fatal("key not found for ts 25")
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}

	// snapshotTS = 30 → v3
	k30 := mvcc.NewMVCCKey(key, 30)
	got, found = mt.Get(k30)
	if !found {
		t.Fatal("key not found for ts 30")
	}
	if string(got) != string(v3) {
		t.Errorf("expected %s, got %s", v3, got)
	}

	// snapshotTS = 5 → not found
	k5 := mvcc.NewMVCCKey(key, 5)
	_, found = mt.Get(k5)
	if found {
		t.Error("expected not found for ts 5")
	}
}

func TestMemTableIterator(t *testing.T) {
	mt := NewMemTable()

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
	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if i >= len(keys) || keys[i] != exp {
			t.Errorf("key %d: expected %s, got %s", i, exp, keys[i])
		}
	}
}

func TestMemTableSize(t *testing.T) {
	mt := NewMemTable()
	if mt.Len() != 0 {
		t.Errorf("initial size should be 0, got %d", mt.Len())
	}

	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 1), []byte("v1"))
	if mt.Len() != 1 {
		t.Errorf("size after one insert should be 1, got %d", mt.Len())
	}

	// New version of same key → size grows (because new node is created)
	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 2), []byte("v2"))
	if mt.Len() != 2 {
		t.Errorf("size after new version should be 2, got %d", mt.Len())
	}
}

func TestMVCCGetWithMultipleVersions(t *testing.T) {
	mt := NewMemTable()
	key := []byte("user1")

	v1 := []byte("value1")
	v2 := []byte("value2")
	v3 := []byte("value3")
	mt.Put(mvcc.NewMVCCKey(key, 10), v1)
	mt.Put(mvcc.NewMVCCKey(key, 20), v2)
	mt.Put(mvcc.NewMVCCKey(key, 30), v3)

	// snapshotTS=15 → v1
	got, found := mt.Get(mvcc.NewMVCCKey(key, 15))
	if !found {
		t.Fatal("key not found for snapshotTS=15")
	}
	if string(got) != string(v1) {
		t.Errorf("expected %s, got %s", v1, got)
	}

	// snapshotTS=25 → v2
	got, found = mt.Get(mvcc.NewMVCCKey(key, 25))
	if !found {
		t.Fatal("key not found for snapshotTS=25")
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}

	// snapshotTS=5 → not found
	_, found = mt.Get(mvcc.NewMVCCKey(key, 5))
	if found {
		t.Error("expected not found for snapshotTS=5")
	}

	// Delete at commitTS=20 (tombstone)
	mt.Delete(mvcc.NewMVCCKey(key, 20))

	// snapshotTS=25 → not found (tombstone hides key)
	_, found = mt.Get(mvcc.NewMVCCKey(key, 25))
	if found {
		t.Error("expected not found for snapshotTS=25 after tombstone")
	}

	// snapshotTS=15 → v1 (tombstone not visible)
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

	mt.Put(mvcc.NewMVCCKey([]byte("foo"), 1), []byte("v1"))
	mt.Put(mvcc.NewMVCCKey([]byte("foo"), 2), []byte("v2"))
	mt.Put(mvcc.NewMVCCKey([]byte("foo"), 3), []byte("v3"))

	tests := []struct {
		snapshotTS uint64
		expected   string
		found      bool
	}{
		{3, "v3", true},
		{2, "v2", true},
		{1, "v1", true},
		{0, "", false},
		{4, "v3", true},
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
// Arena tests (unchanged, work with new Arena)
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

	p3 := a.Alloc(0)
	if p3 == nil {
		t.Fatal("Alloc(0) returned nil")
	}
}

func TestArenaReset(t *testing.T) {
	a := NewArena()

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

	a.Reset()

	if a.Size() != 0 {
		t.Errorf("expected Size()=0 after Reset, got %d", a.Size())
	}
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block after Reset, got %d", a.NumBlocks())
	}

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
	a.Reset()
	if a.Size() != 0 {
		t.Errorf("expected Size()=0 after Reset on empty, got %d", a.Size())
	}
	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 block after Reset on empty, got %d", a.NumBlocks())
	}
}

func TestArenaSingleFlatBlock(t *testing.T) {
	a := NewArena()

	// Flat arena is a single grow‑only block; allocations stay within it.
	for i := 0; i < 3; i++ {
		_ = a.Alloc(64)
	}

	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 flat block, got %d", a.NumBlocks())
	}

	totalSize := a.Size()
	if totalSize == 0 {
		t.Error("expected Size() > 0 after allocations")
	}
	if totalSize < 3*64 {
		t.Errorf("expected Size() >= %d, got %d", 3*64, totalSize)
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

	nodeKey := node.Key()
	if string(nodeKey.Key) != string(key) {
		t.Errorf("expected key %s, got %s", key, nodeKey.Key)
	}

	nodeVal := node.Value()
	if string(nodeVal) != string(value) {
		t.Errorf("expected value %s, got %s", value, nodeVal)
	}

	if node.height != 10 {
		t.Errorf("expected height 10, got %d", node.height)
	}

	if node.deleted != 0 {
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

	_ = a.Alloc(100)
	a.Reset()

	if a.Size() != 0 {
		t.Errorf("expected Size()=0 after Reset, got %d", a.Size())
	}

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

	// Flat arena keeps a single block across allocations.
	_ = a.Alloc(128)
	_ = a.Alloc(256)
	_ = a.Alloc(512)

	if a.NumBlocks() != 1 {
		t.Errorf("expected 1 flat block after allocations, got %d", a.NumBlocks())
	}

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
// MemTable additional tests (adapted to new API)
// ============================================================

func TestMemTableGetLatest(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	// Empty table
	_, _, found := mt.GetLatest(key)
	if found {
		t.Error("expected not found on empty table")
	}

	// Insert one version
	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value1"))
	val, ts, found := mt.GetLatest(key)
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

	// Insert older version
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

	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value1"))
	mt.Delete(mvcc.NewMVCCKey(key, 100))

	_, _, found := mt.GetLatest(key)
	if found {
		t.Error("expected not found after tombstone")
	}
}

func TestMemTableGetLatestMultipleVersionsWithTombstone(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value1"))
	mt.Put(mvcc.NewMVCCKey(key, 200), []byte("value2"))
	mt.Delete(mvcc.NewMVCCKey(key, 200))

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

	// Insert keys
	mt.Put(mvcc.NewMVCCKey([]byte("a"), 100), []byte("val_a"))
	mt.Put(mvcc.NewMVCCKey([]byte("b"), 100), []byte("val_b"))
	mt.Put(mvcc.NewMVCCKey([]byte("c"), 100), []byte("val_c"))

	lastKey = mt.LastKey()
	if string(lastKey.Key) != "c" {
		t.Errorf("expected last key 'c', got '%s'", lastKey.Key)
	}
}

func TestMemTableClose(t *testing.T) {
	mt := NewMemTable()

	mt.Put(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value"))

	mt.Close()

	_, found := mt.Get(mvcc.NewMVCCKey([]byte("key"), 100))
	if found {
		t.Error("expected not found after Close")
	}

	_, _, found = mt.GetLatest([]byte("key"))
	if found {
		t.Error("expected not found from GetLatest after Close")
	}

	if mt.Len() != 0 {
		t.Errorf("expected Len()=0 after Close, got %d", mt.Len())
	}
}

func TestMemTableCloseEmpty(t *testing.T) {
	mt := NewMemTable()
	mt.Close()
}

func TestMemTableCloseTwice(t *testing.T) {
	mt := NewMemTable()
	mt.Close()
	mt.Close()
}

func TestMemTableDeleteWithTS(t *testing.T) {
	mt := NewMemTable()
	key := []byte("test_key")

	mt.Put(mvcc.NewMVCCKey(key, 100), []byte("value"))
	// Delete at a commitTS with no prior live version must still place a
	// tombstone that hides the key at snapshots >= 200.
	if !mt.DeleteWithTS(mvcc.NewMVCCKey(key, 200)) {
		t.Fatal("DeleteWithTS should place a tombstone")
	}

	_, found := mt.Get(mvcc.NewMVCCKey(key, 50))
	if found {
		t.Error("expected NOT found at snapshot 50")
	}

	val, found := mt.Get(mvcc.NewMVCCKey(key, 150))
	if !found {
		t.Fatal("expected found at snapshot 150")
	}
	if string(val) != "value" {
		t.Errorf("expected 'value', got '%s'", val)
	}

	_, found = mt.Get(mvcc.NewMVCCKey(key, 250))
	if found {
		t.Error("expected NOT found at snapshot 250")
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

	mt.Delete(mvcc.NewMVCCKey([]byte("b"), 100))

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

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				mt.Get(mvcc.NewMVCCKey([]byte("shared_key"), uint64(j)))
			}
		}()
	}

	wg.Wait()
}

// TestDataRaceDeletedNext verifies that concurrent writers (Put/Delete) and
// readers (GetLatest) do not cause a data race on node.deleted / node.next.
//
// Readers must use atomic.LoadUint32 for `deleted` and `next` because writers
// store them via atomic.StoreUint32. Non-atomic reads
// are flagged by the -race detector. This test exercises that path with 5
// writers and 10 readers for ~1 second.
func TestDataRaceDeletedNext(t *testing.T) {
	mt := NewMemTable()

	// Insert 10 keys with multiple MVCC versions.
	const numKeys = 10
	const versions = 3
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("key:%d", i))
		for v := 0; v < versions; v++ {
			mt.Put(mvcc.NewMVCCKey(key, uint64(v+1)), []byte(fmt.Sprintf("v%d", v)))
		}
	}

	start := time.Now()
	var wg sync.WaitGroup
	done := make(chan struct{})

	// 5 writers cycling Put and Delete.
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			i := seed
			for {
				select {
				case <-done:
					return
				default:
				}
				key := []byte(fmt.Sprintf("key:%d", i%numKeys))
				ts := uint64((i/numKeys)%versions) + 1
				if i%2 == 0 {
					mt.Put(mvcc.NewMVCCKey(key, ts), []byte("new_value"))
				} else {
					mt.Delete(mvcc.NewMVCCKey(key, ts))
				}
				i++
			}
		}(w)
	}

	// 10 readers calling GetLatest in a loop.
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func(seed int) {
			defer wg.Done()
			i := seed
			for {
				select {
				case <-done:
					return
				default:
				}
				key := []byte(fmt.Sprintf("key:%d", i%numKeys))
				mt.GetLatest(key)
				i++
			}
		}(r)
	}

	time.Sleep(1 * time.Second)
	close(done)
	wg.Wait()

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("TestDataRaceDeletedNext took %v, expected < 1s work", elapsed)
	}
}
