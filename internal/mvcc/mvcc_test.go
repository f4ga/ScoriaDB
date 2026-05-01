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

package mvcc

import (
	"sync"
	"testing"
)

func TestTimestampMonotonic(t *testing.T) {
	tg := NewTimestampGenerator()
	prev := tg.Next()
	if prev != 1 {
		t.Errorf("expected initial timestamp 1, got %d", prev)
	}
	// Increment a few times and ensure monotonic increase
	for i := 0; i < 100; i++ {
		next := tg.Increment()
		if next <= prev {
			t.Errorf("timestamp not monotonic: prev=%d, next=%d", prev, next)
		}
		prev = next
	}
}

func TestTimestampConcurrency(t *testing.T) {
	tg := NewTimestampGenerator()
	const goroutines = 100
	const callsPerGoroutine = 1000
	results := make(chan uint64, goroutines*callsPerGoroutine)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < callsPerGoroutine; i++ {
				ts := tg.Increment()
				results <- ts
			}
		}()
	}
	wg.Wait()
	close(results)

	// Collect all timestamps
	seen := make(map[uint64]bool)
	for ts := range results {
		if seen[ts] {
			t.Errorf("duplicate timestamp %d", ts)
		}
		seen[ts] = true
	}

	// Verify that timestamps are sequential from 2 to N+1
	// Since we start at 1 and each Increment adds 1, total increments = goroutines * callsPerGoroutine
	expectedCount := goroutines * callsPerGoroutine
	if len(seen) != expectedCount {
		t.Errorf("expected %d unique timestamps, got %d", expectedCount, len(seen))
	}
	// Check that max timestamp equals expectedCount + 1 (since start at 1, first increment returns 2)
	maxTS := uint64(0)
	for ts := range seen {
		if ts > maxTS {
			maxTS = ts
		}
	}
	expectedMax := uint64(expectedCount + 1)
	if maxTS != expectedMax {
		t.Errorf("max timestamp expected %d, got %d", expectedMax, maxTS)
	}
}

func TestTimestampSet(t *testing.T) {
	tg := NewTimestampGenerator()
	// Initial value should be 1
	if v := tg.Next(); v != 1 {
		t.Errorf("expected 1, got %d", v)
	}
	// Set to higher value
	tg.Set(100)
	if v := tg.Next(); v != 100 {
		t.Errorf("expected 100 after Set, got %d", v)
	}
	// Set to lower value should be ignored
	tg.Set(50)
	if v := tg.Next(); v != 100 {
		t.Errorf("expected still 100 after lower Set, got %d", v)
	}
	// Concurrent Set and Increment
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		tg.Set(200)
	}()
	go func() {
		defer wg.Done()
		tg.Increment()
	}()
	wg.Wait()
	// After concurrent operations, the counter should be at least 200 or more
	// (since Increment may have increased it)
	v := tg.Next()
	if v < 200 {
		t.Errorf("expected at least 200 after concurrent Set/Increment, got %d", v)
	}
}
