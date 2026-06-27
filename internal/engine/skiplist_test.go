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
	"fmt"
	"sync"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// Helper to create an MVCCKey with a given string key and timestamp.
func testKey(s string, ts uint64) mvcc.MVCCKey {
	return mvcc.NewMVCCKey([]byte(s), ts)
}

// TestSkipListBasic tests basic operations: Put, Get, Delete.
func TestSkipListBasic(t *testing.T) {
	sl := NewSkipList()

	// Test Put and Get
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

	// Test Get non-existent key
	_, ok = sl.Get(testKey("nonexistent", 1))
	if ok {
		t.Errorf("expected false for non-existent key")
	}

	// Test Delete
	if !sl.Delete(testKey("key1", 1)) {
		t.Errorf("expected Delete to return true")
	}
	_, ok = sl.Get(testKey("key1", 1))
	if ok {
		t.Errorf("expected key1 to be deleted")
	}

	// Test Delete non-existent key
	if sl.Delete(testKey("nonexistent", 1)) {
		t.Errorf("expected Delete to return false for non-existent key")
	}
}

// TestSkipListUpdate tests updating an existing key.
func TestSkipListUpdate(t *testing.T) {
	sl := NewSkipList()

	sl.Put(testKey("key", 1), []byte("value1"))
	sl.Put(testKey("key", 2), []byte("value2"))

	// Get should return the latest version (highest timestamp)
	val, ok := sl.Get(testKey("key", 2))
	if !ok || string(val) != "value2" {
		t.Errorf("expected value2, got %s", string(val))
	}
}

// TestSkipListConcurrent tests concurrent Put and Get operations.
func TestSkipListConcurrent(t *testing.T) {
	sl := NewSkipList()
	var wg sync.WaitGroup
	n := 100

	// Concurrent writes
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := testKey(fmt.Sprintf("key:%d", id), 1)
			sl.Put(key, []byte(fmt.Sprintf("value:%d", id)))
		}(i)
	}
	wg.Wait()

	// Verify all writes
	for i := 0; i < n; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
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

	// Concurrent reads
	var wg2 sync.WaitGroup
	for i := 0; i < n; i++ {
		wg2.Add(1)
		go func(id int) {
			defer wg2.Done()
			key := testKey(fmt.Sprintf("key:%d", id), 1)
			val, ok := sl.Get(key)
			if !ok {
				t.Errorf("concurrent read: key %s not found", string(key.Key))
				return
			}
			expected := fmt.Sprintf("value:%d", id)
			if string(val) != expected {
				t.Errorf("concurrent read: expected %s, got %s", expected, string(val))
			}
		}(i)
	}
	wg2.Wait()
}

// TestSkipListIterator tests the lock-free iterator.
func TestSkipListIterator(t *testing.T) {
	sl := NewSkipList()

	// Insert keys in reverse order to test ordering
	keys := []string{"a", "b", "c", "d", "e"}
	for i, k := range keys {
		sl.Put(testKey(k, 1), []byte(fmt.Sprintf("value:%s", k)))
		_ = i
	}

	// Iterate and verify order
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

// TestSkipListIteratorDeleted tests that iterator skips deleted nodes.
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

// TestSkipListLen tests the Len() method.
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

// TestSkipListRandomHeight tests that randomHeight produces valid heights.
func TestSkipListRandomHeight(t *testing.T) {
	for i := 0; i < 1000; i++ {
		h := randomHeight()
		if h < 1 || h > maxHeight {
			t.Errorf("height %d out of range [1, %d]", h, maxHeight)
		}
	}
}

// TestSkipListLarge tests the skip list with a large number of entries.
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

	// Verify all entries
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

// BenchmarkSkipListGet benchmarks Get operations.
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

// BenchmarkSkipListPut benchmarks Put operations.
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

// BenchmarkSkipListPutSequential benchmarks sequential Put operations.
func BenchmarkSkipListPutSequential(b *testing.B) {
	sl := NewSkipList()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := testKey(fmt.Sprintf("key:%d", i), 1)
		sl.Put(key, []byte("value"))
	}
}

// BenchmarkSkipListGetSequential benchmarks sequential Get operations.
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
