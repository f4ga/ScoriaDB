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

	seen := make(map[uint64]bool)
	for ts := range results {
		if seen[ts] {
			t.Errorf("duplicate timestamp %d", ts)
		}
		seen[ts] = true
	}

	expectedCount := goroutines * callsPerGoroutine
	if len(seen) != expectedCount {
		t.Errorf("expected %d unique timestamps, got %d", expectedCount, len(seen))
	}
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
	if v := tg.Next(); v != 1 {
		t.Errorf("expected 1, got %d", v)
	}
	tg.Set(100)
	if v := tg.Next(); v != 100 {
		t.Errorf("expected 100 after Set, got %d", v)
	}
	tg.Set(50)
	if v := tg.Next(); v != 100 {
		t.Errorf("expected still 100 after lower Set, got %d", v)
	}
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
	v := tg.Next()
	if v < 200 {
		t.Errorf("expected at least 200 after concurrent Set/Increment, got %d", v)
	}
}

func TestMVCCKeyCompare(t *testing.T) {
	tests := []struct {
		name     string
		a, b     MVCCKey
		expected int
	}{
		{
			name:     "equal keys and timestamps",
			a:        NewMVCCKey([]byte("key"), 100),
			b:        NewMVCCKey([]byte("key"), 100),
			expected: 0,
		},
		{
			name:     "same key, different timestamp (newer first)",
			a:        NewMVCCKey([]byte("key"), 200),
			b:        NewMVCCKey([]byte("key"), 100),
			expected: 1,
		},
		{
			name:     "different keys",
			a:        NewMVCCKey([]byte("a"), 100),
			b:        NewMVCCKey([]byte("b"), 100),
			expected: -1,
		},
		{
			name:     "same key, same timestamp",
			a:        NewMVCCKey([]byte("key"), 100),
			b:        NewMVCCKey([]byte("key"), 100),
			expected: 0,
		},
		{
			name:     "key a less than b",
			a:        NewMVCCKey([]byte("apple"), 100),
			b:        NewMVCCKey([]byte("banana"), 100),
			expected: -1,
		},
		{
			name:     "key a greater than b",
			a:        NewMVCCKey([]byte("banana"), 100),
			b:        NewMVCCKey([]byte("apple"), 100),
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.a.Compare(tt.b)
			if result != tt.expected {
				t.Errorf("Compare() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestMVCCKeyCommitTS(t *testing.T) {
	key := NewMVCCKey([]byte("key"), 12345)
	if key.CommitTS() != 12345 {
		t.Errorf("CommitTS() = %d, want 12345", key.CommitTS())
	}
}

func TestNewMVCCKey(t *testing.T) {
	key := []byte("test")
	ts := uint64(999)
	mvccKey := NewMVCCKey(key, ts)

	if string(mvccKey.Key) != string(key) {
		t.Errorf("Key = %s, want %s", mvccKey.Key, key)
	}
	if mvccKey.CommitTS() != ts {
		t.Errorf("CommitTS = %d, want %d", mvccKey.CommitTS(), ts)
	}
}

func TestInvertRevertTimestamp(t *testing.T) {
	original := uint64(12345)
	inverted := InvertTimestamp(original)
	reverted := RevertTimestamp(inverted)

	if reverted != original {
		t.Errorf("RevertTimestamp(InvertTimestamp(%d)) = %d, want %d", original, reverted, original)
	}
	if inverted == original {
		t.Error("InvertTimestamp should change the value")
	}
}
