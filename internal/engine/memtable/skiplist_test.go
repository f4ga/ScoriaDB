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

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

func testKey(s string, ts uint64) mvcc.MVCCKey {
	return mvcc.NewMVCCKey([]byte(s), ts)
}

func TestSkipListBasic(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("key1", 1), []byte("value1"))
	sl.Put(testKey("key2", 1), []byte("value2"))
	sl.Put(testKey("key3", 1), []byte("value3"))

	val, ok := sl.Get(testKey("key1", 1))
	if !ok || string(val) != "value1" {
		t.Errorf("expected value1, got %s (ok=%v)", string(val), ok)
	}

	val, ok = sl.Get(testKey("key2", 1))
	if !ok || string(val) != "value2" {
		t.Errorf("expected value2, got %s (ok=%v)", string(val), ok)
	}

	_, ok = sl.Get(testKey("nonexistent", 1))
	if ok {
		t.Errorf("expected false for non-existent key")
	}

	if !sl.Delete(testKey("key1", 1)) {
		t.Errorf("expected Delete to return true")
	}
	_, ok = sl.Get(testKey("key1", 1))
	if ok {
		t.Errorf("expected key1 to be deleted")
	}

	if sl.Delete(testKey("nonexistent", 1)) {
		t.Errorf("expected Delete to return false for non-existent key")
	}
}

func TestSkipListUpdate(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("key", 1), []byte("value1"))
	sl.Put(testKey("key", 1), []byte("value2"))

	val, ok := sl.Get(testKey("key", 1))
	if !ok || string(val) != "value2" {
		t.Errorf("expected value2, got %s", string(val))
	}
}

func TestSkipListMultipleVersions(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("key", 1), []byte("v1"))
	sl.Put(testKey("key", 2), []byte("v2"))
	sl.Put(testKey("key", 3), []byte("v3"))

	val, ok := sl.Get(testKey("key", 1))
	if !ok || string(val) != "v1" {
		t.Errorf("expected v1, got %s", string(val))
	}

	val, ok = sl.Get(testKey("key", 2))
	if !ok || string(val) != "v2" {
		t.Errorf("expected v2, got %s", string(val))
	}

	val, ok = sl.Get(testKey("key", 3))
	if !ok || string(val) != "v3" {
		t.Errorf("expected v3, got %s", string(val))
	}

	_, ok = sl.Get(testKey("key", 999))
	if ok {
		t.Errorf("expected false for non-existent timestamp")
	}
}

func TestSkipListConcurrent(t *testing.T) {
	sl := NewSkipList()
	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := testKey(fmt.Sprintf("key:%d", id), 1)
			sl.Put(key, []byte(fmt.Sprintf("value:%d", id)))
		}(i)
	}
	wg.Wait()

	t.Logf("After puts: Len()=%d", sl.Len())

	missing := 0
	for i := 0; i < n; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
		val, ok := sl.Get(key)
		if !ok {
			t.Errorf("key %s not found", string(key.Key))
			missing++
			continue
		}
		expected := fmt.Sprintf("value:%d", i)
		if string(val) != expected {
			t.Errorf("expected %s, got %s", expected, string(val))
		}
	}

	t.Logf("Missing keys: %d/%d", missing, n)
}

func TestSkipListConcurrentPutGet(t *testing.T) {
	sl := NewSkipList()
	var wg sync.WaitGroup
	numWorkers := 10
	numOps := 100

	// Concurrent writes from multiple goroutines
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := mvcc.NewMVCCKey([]byte("key"), uint64(id*numOps+j))
				sl.Put(key, []byte("value"))
			}
		}(i)
	}
	wg.Wait()

	// Verify all keys are readable
	for i := 0; i < numWorkers; i++ {
		for j := 0; j < numOps; j++ {
			key := mvcc.NewMVCCKey([]byte("key"), uint64(i*numOps+j))
			val, found := sl.Get(key)
			if !found {
				t.Errorf("key not found: %v", key)
			}
			if val == nil {
				t.Errorf("value is nil for key: %v", key)
			}
		}
	}
}

func TestSkipListIterator(t *testing.T) {
	sl := NewSkipList()

	keys := []string{"a", "b", "c", "d", "e"}
	for _, k := range keys {
		sl.Put(testKey(k, 1), []byte(fmt.Sprintf("value:%s", k)))
	}

	it := sl.NewIterator()
	count := 0
	for it.Next() {
		key := it.Key()
		val := it.Value()
		if count >= len(keys) {
			t.Errorf("too many elements")
			break
		}
		expectedKey := keys[count]
		if string(key.Key) != expectedKey {
			t.Errorf("expected key %s, got %s", expectedKey, string(key.Key))
		}
		expectedVal := fmt.Sprintf("value:%s", expectedKey)
		if string(val) != expectedVal {
			t.Errorf("expected value %s, got %s", expectedVal, string(val))
		}
		count++
	}
	it.Close()

	if count != len(keys) {
		t.Errorf("expected %d elements, got %d", len(keys), count)
	}
}

func TestSkipListIteratorDeleted(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("a", 1), []byte("value_a"))
	sl.Put(testKey("b", 1), []byte("value_b"))
	sl.Put(testKey("c", 1), []byte("value_c"))

	sl.Delete(testKey("b", 1))

	it := sl.NewIterator()
	var results []string
	for it.Next() {
		results = append(results, string(it.Key().Key))
	}
	it.Close()

	if len(results) != 2 {
		t.Errorf("expected 2 elements after delete, got %d", len(results))
	}
	if results[0] != "a" || results[1] != "c" {
		t.Errorf("expected [a c], got %v", results)
	}
}

func TestSkipListLen(t *testing.T) {
	sl := NewSkipList()

	if sl.Len() != 0 {
		t.Errorf("expected 0, got %d", sl.Len())
	}

	sl.Put(testKey("a", 1), []byte("1"))
	sl.Put(testKey("b", 1), []byte("2"))
	sl.Put(testKey("c", 1), []byte("3"))

	if sl.Len() != 3 {
		t.Errorf("expected 3, got %d", sl.Len())
	}

	sl.Delete(testKey("a", 1))
	if sl.Len() != 2 {
		t.Errorf("expected 2 after delete, got %d", sl.Len())
	}
}

func TestSkipListRandomHeight(t *testing.T) {
	for i := 0; i < 1000; i++ {
		h := randomHeight()
		if h < 1 || h > MaxHeight {
			t.Errorf("height %d out of range [1, %d]", h, MaxHeight)
		}
	}
}

func TestSkipListLarge(t *testing.T) {
	sl := NewSkipList()
	n := 10000

	for i := 0; i < n; i++ {
		key := testKey(fmt.Sprintf("key:%08d", i), 1)
		sl.Put(key, []byte(fmt.Sprintf("value:%d", i)))
	}

	if sl.Len() != n {
		t.Errorf("expected %d elements, got %d", n, sl.Len())
	}

	for i := 0; i < n; i++ {
		key := testKey(fmt.Sprintf("key:%08d", i), 1)
		val, ok := sl.Get(key)
		if !ok {
			t.Errorf("key %s not found", string(key.Key))
			continue
		}
		expected := fmt.Sprintf("value:%d", i)
		if string(val) != expected {
			t.Errorf("expected %s, got %s", expected, string(val))
		}
	}
}

func BenchmarkSkipListGet(b *testing.B) {
	sl := NewSkipList()
	for i := 0; i < 100000; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
		sl.Put(key, []byte("value"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := testKey(fmt.Sprintf("key:%d", i%100000), 1)
			sl.Get(key)
			i++
		}
	})
}

func BenchmarkSkipListPut(b *testing.B) {
	sl := NewSkipList()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := testKey(fmt.Sprintf("key:%d", i), 1)
			sl.Put(key, []byte("value"))
			i++
		}
	})
}

func BenchmarkSkipListPutSequential(b *testing.B) {
	sl := NewSkipList()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
		sl.Put(key, []byte("value"))
	}
}

func BenchmarkSkipListGetSequential(b *testing.B) {
	sl := NewSkipList()
	for i := 0; i < 100000; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
		sl.Put(key, []byte("value"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := testKey(fmt.Sprintf("key:%d", i%100000), 1)
		sl.Get(key)
	}
}

// ============================================================
// SkipList additional tests
// ============================================================

func TestSkipListFindLast(t *testing.T) {
	sl := NewSkipList()

	// Empty list — findLast should return head
	last := sl.findLast()
	if last == nil {
		t.Fatal("findLast on empty list returned nil")
	}
	if last != sl.head {
		t.Error("findLast on empty list should return head")
	}

	// Single node
	sl.Put(testKey("a", 100), []byte("val_a"))
	last = sl.findLast()
	if last == nil {
		t.Fatal("findLast returned nil after insert")
	}
	lastKey := last.Key()
	if string(lastKey.Key) != "a" {
		t.Errorf("expected last key 'a', got '%s'", lastKey.Key)
	}

	// Multiple nodes
	sl.Put(testKey("b", 100), []byte("val_b"))
	sl.Put(testKey("c", 100), []byte("val_c"))
	last = sl.findLast()
	if last == nil {
		t.Fatal("findLast returned nil after multiple inserts")
	}
	lastKey = last.Key()
	if string(lastKey.Key) != "c" {
		t.Errorf("expected last key 'c', got '%s'", lastKey.Key)
	}
}

func TestSkipListFindLastAfterDelete(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("a", 100), []byte("val_a"))
	sl.Put(testKey("b", 100), []byte("val_b"))
	sl.Put(testKey("c", 100), []byte("val_c"))

	// Delete the last key
	sl.Delete(testKey("c", 100))

	// findLast should return the previous max key
	last := sl.findLast()
	if last == nil {
		t.Fatal("findLast returned nil after delete")
	}
	lastKey := last.Key()
	if string(lastKey.Key) != "b" {
		t.Errorf("expected last key 'b' after delete, got '%s'", lastKey.Key)
	}
}

func TestSkipListHeight(t *testing.T) {
	sl := NewSkipList()

	// New list has height 1
	if sl.Height() != 1 {
		t.Errorf("expected height 1 for new list, got %d", sl.Height())
	}

	// Insert many nodes — height should increase
	for i := 0; i < 1000; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
		sl.Put(key, []byte("value"))
	}

	h := sl.Height()
	if h < 1 || h > MaxHeight {
		t.Errorf("height %d out of range [1, %d]", h, MaxHeight)
	}
	t.Logf("SkipList height after 1000 inserts: %d", h)
}

func TestSkipListArena(t *testing.T) {
	sl := NewSkipList()

	arena := sl.Arena()
	if arena == nil {
		t.Fatal("Arena() returned nil")
	}

	// Arena should be usable
	ptr := arena.Alloc(8)
	if ptr == nil {
		t.Error("Arena.Alloc via SkipList returned nil")
	}
}

func TestSkipListFindGreaterOrEqual(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("a", 100), []byte("val_a"))
	sl.Put(testKey("b", 100), []byte("val_b"))
	sl.Put(testKey("c", 100), []byte("val_c"))

	// Find existing key
	node := sl.findGreaterOrEqual(testKey("b", 100))
	if node == nil {
		t.Fatal("findGreaterOrEqual returned nil for existing key")
	}
	nodeKey := node.Key()
	if string(nodeKey.Key) != "b" {
		t.Errorf("expected key 'b', got '%s'", nodeKey.Key)
	}

	// Find non-existent key (should return next greater)
	node = sl.findGreaterOrEqual(testKey("bb", 100))
	if node == nil {
		t.Fatal("findGreaterOrEqual returned nil for non-existent key")
	}
	nodeKey = node.Key()
	if string(nodeKey.Key) != "c" {
		t.Errorf("expected key 'c' (next greater), got '%s'", nodeKey.Key)
	}

	// Find key greater than all existing
	node = sl.findGreaterOrEqual(testKey("z", 100))
	if node != nil {
		t.Errorf("expected nil for key beyond max, got key '%s'", string(node.Key().Key))
	}
}

func TestSkipListPutDuplicateTimestamp(t *testing.T) {
	sl := NewSkipList()

	// Put same key+timestamp twice — should update in-place
	sl.Put(testKey("key", 100), []byte("value1"))
	sl.Put(testKey("key", 100), []byte("value2"))

	val, ok := sl.Get(testKey("key", 100))
	if !ok {
		t.Fatal("key not found after duplicate put")
	}
	if string(val) != "value2" {
		t.Errorf("expected 'value2', got '%s'", val)
	}

	// Len should be 1 (not 2)
	if sl.Len() != 1 {
		t.Errorf("expected Len()=1 after duplicate put, got %d", sl.Len())
	}
}

func TestSkipListDeleteNonExistent(t *testing.T) {
	sl := NewSkipList()

	// Delete on empty list
	if sl.Delete(testKey("nonexistent", 100)) {
		t.Error("expected Delete to return false for non-existent key")
	}

	// Delete with wrong timestamp
	sl.Put(testKey("key", 100), []byte("value"))
	if sl.Delete(testKey("key", 200)) {
		t.Error("expected Delete to return false for wrong timestamp")
	}
}

func TestSkipListDeleteTwice(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("key", 100), []byte("value"))

	if !sl.Delete(testKey("key", 100)) {
		t.Fatal("first Delete should return true")
	}
	if sl.Delete(testKey("key", 100)) {
		t.Error("second Delete should return false")
	}
}

func TestSkipListIteratorEmpty(t *testing.T) {
	sl := NewSkipList()

	it := sl.NewIterator()
	defer it.Close()

	if it.Next() {
		t.Error("expected no items from empty iterator")
	}
}

func TestSkipListIteratorAfterExhaustion(t *testing.T) {
	sl := NewSkipList()
	sl.Put(testKey("a", 100), []byte("val_a"))

	it := sl.NewIterator()
	defer it.Close()

	// Consume all items
	if !it.Next() {
		t.Fatal("expected one item")
	}
	if it.Next() {
		t.Error("expected false after exhaustion")
	}
}

func TestSkipListIteratorClose(t *testing.T) {
	sl := NewSkipList()
	sl.Put(testKey("a", 100), []byte("val_a"))

	it := sl.NewIterator()
	it.Close()

	// Next should return false after close
	if it.Next() {
		t.Error("expected false after close")
	}

	// Double close should not panic
	it.Close()
}

func TestSkipListIteratorIsDeleted(t *testing.T) {
	sl := NewSkipList()
	sl.Put(testKey("a", 100), []byte("val_a"))

	it := sl.NewIterator()
	defer it.Close()

	if it.Next() {
		if it.IsDeleted() {
			t.Error("expected IsDeleted=false for active entry")
		}
	}
}

func TestSkipListConcurrentReadWrite(t *testing.T) {
	sl := NewSkipList()
	var wg sync.WaitGroup
	numOps := 100

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				key := testKey(fmt.Sprintf("key:%d", id*numOps+j), 1)
				sl.Put(key, []byte("value"))
			}
		}(i)
	}

	// Concurrent readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOps; j++ {
				sl.Get(testKey(fmt.Sprintf("key:%d", j), 1))
			}
		}()
	}

	wg.Wait()
}

func TestSkipListConcurrentDelete(t *testing.T) {
	sl := NewSkipList()
	var wg sync.WaitGroup
	numKeys := 100

	// Insert keys
	for i := 0; i < numKeys; i++ {
		sl.Put(testKey(fmt.Sprintf("key:%d", i), 1), []byte("value"))
	}

	// Concurrent deletes
	for i := 0; i < numKeys; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sl.Delete(testKey(fmt.Sprintf("key:%d", id), 1))
		}(i)
	}
	wg.Wait()

	// All keys should be deleted
	if sl.Len() != 0 {
		t.Errorf("expected Len()=0 after all deletes, got %d", sl.Len())
	}
}
