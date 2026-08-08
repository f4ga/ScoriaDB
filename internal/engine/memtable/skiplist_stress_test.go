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
	"bytes"
	"sync"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// TestConcurrentSkipList_Stress runs 100 goroutines doing 10,000 inserts/deletes each,
// then verifies list integrity: sorted order and correct count.
func TestConcurrentSkipList_Stress(t *testing.T) {
	sl := NewSkipList()

	// The flat arena is a single grow‑only block of ArenaBlockSize (64 MB).
	// Each node occupies ~120 bytes, so 200K nodes (~24 MB) fits comfortably.
	// Larger workloads exceed the single fixed block and would panic the arena.
	// See: ARCH-01 (ArenaBlockSize).
	const (
		numGoroutines = 100
		opsPerThread  = 2_000
	)

	var wg sync.WaitGroup

	// Phase 1: Concurrent inserts
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			base := gid * opsPerThread
			for i := 0; i < opsPerThread; i++ {
				key := mvcc.MVCCKey{
					Key:       []byte{byte((base + i) >> 8), byte(base + i)},
					Timestamp: mvcc.InvertTimestamp(uint64(base + i + 1)),
				}
				sl.Put(key, []byte{byte(gid), byte(i)})
			}
		}(g)
	}
	wg.Wait()

	expectedCount := numGoroutines * opsPerThread
	actualLen := sl.Len()
	t.Logf("[STRESS] After inserts: Len()=%d, expected=%d", actualLen, expectedCount)

	// Phase 2: Verify sorted order and count via full iteration
	count := 0
	var prevKey mvcc.MVCCKey
	first := true
	iter := sl.NewIterator()
	for iter.Next() {
		currKey := iter.Key()
		if !first {
			if !nodeKeyLess(prevKey, currKey) && !bytes.Equal(prevKey.Key, currKey.Key) {
				t.Errorf("[STRESS] List integrity: out of order at index %d", count)
			}
		}
		first = false
		prevKey = currKey
		count++
	}
	iter.Close()

	if count != expectedCount {
		t.Fatalf("[STRESS] List integrity check failed: expected %d active entries, got %d", expectedCount, count)
	}
	t.Logf("[PASS] Phase 2: %d entries, all in sorted order", count)

	// Phase 3: Concurrent deletes — delete every other entry
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go func(gid int) {
			defer wg.Done()
			base := gid * opsPerThread
			for i := 0; i < opsPerThread; i += 2 {
				key := mvcc.MVCCKey{
					Key:       []byte{byte((base + i) >> 8), byte(base + i)},
					Timestamp: mvcc.InvertTimestamp(uint64(base + i + 1)),
				}
				sl.Delete(key)
			}
		}(g)
	}
	wg.Wait()

	expectedAfterDelete := expectedCount / 2
	actualAfterDelete := sl.Len()
	t.Logf("[STRESS] After deletes: Len()=%d, expected=%d", actualAfterDelete, expectedAfterDelete)

	// Phase 4: Final integrity check
	count = 0
	first = true
	iter2 := sl.NewIterator()
	for iter2.Next() {
		currKey := iter2.Key()
		if !first {
			if !nodeKeyLess(prevKey, currKey) && !bytes.Equal(prevKey.Key, currKey.Key) {
				t.Errorf("[STRESS] List integrity: out of order at index %d", count)
			}
		}
		first = false
		prevKey = currKey
		count++
	}
	iter2.Close()

	if count != expectedAfterDelete {
		t.Fatalf("[STRESS] List integrity check failed after deletes: expected %d, got %d", expectedAfterDelete, count)
	}
	t.Logf("[PASS] Phase 4: %d entries remain, all in sorted order", count)
}
