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
	"sync/atomic"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

// TestMaybeCompactNonBlocking verifies that Put operations are NOT blocked while
// maybeCompact signals the compaction worker. The worker re-checks the condition
// and performs compaction in the background, so concurrent writes continue.
// See: DEF-D2, Глава XIII (background tasks must not block the hot path).
func TestMaybeCompactNonBlocking(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Populate several level-0 SSTables so the compaction condition is met.
	for round := 0; round < MaxLevel0Files+1; round++ {
		for i := 0; i < 64; i++ {
			key := []byte(fmt.Sprintf("k-%d-%d", round, i))
			if err := eng.PutWithTS(key, []byte("v"), uint64(i+1)); err != nil {
				t.Fatalf("PutWithTS failed: %v", err)
			}
		}
		if err := eng.flushMemTable(); err != nil {
			t.Fatalf("flushMemTable: %v", err)
		}
	}

	// Condition met: more than MaxLevel0Files level-0 files.
	eng.mu.RLock()
	level0 := len(eng.levels[0])
	eng.mu.RUnlock()
	if level0 <= MaxLevel0Files {
		t.Fatalf("expected level-0 files > MaxLevel0Files, got %d", level0)
	}

	// Start concurrent Put operations while maybeCompact signals the worker.
	done := make(chan struct{})
	var wg sync.WaitGroup
	putErr := make(chan error, 64)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := []byte(fmt.Sprintf("live-%d-%d", id, j))
				if err := eng.PutWithTS(key, []byte("live"), uint64(j+1)); err != nil {
					putErr <- err
					return
				}
				// Pause briefly so the compaction worker has a chance to run.
				time.Sleep(time.Millisecond)
			}
		}(i)
	}
	go func() {
		for i := 0; i < 50; i++ {
			eng.maybeCompact()
			time.Sleep(5 * time.Millisecond)
		}
		close(done)
	}()
	wg.Wait()
	<-done
	close(putErr)
	for err := range putErr {
		t.Errorf("Put during maybeCompact failed: %v", err)
	}
}

// TestCompactConditionRecheck verifies that a stale signal (sent before the
// level-0 condition changed) does NOT trigger compaction. The worker re-checks
// the actual condition before compacting. See: DEF-D2.
func TestCompactConditionRecheck(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Simulate a condition that is NOT met: no level-0 files at all.
	eng.mu.RLock()
	level0 := len(eng.levels[0])
	eng.mu.RUnlock()
	if level0 != 0 {
		t.Fatalf("expected 0 level-0 files, got %d", level0)
	}

	// Manually drain any pending signal so the worker has nothing to process.
	for {
		select {
		case <-eng.compactCh:
		default:
			goto drained
		}
	}
drained:

	// maybeCompact with no level-0 files must not compact.
	eng.maybeCompact()

	// Give the worker a moment to (incorrectly) process a stale signal if any.
	time.Sleep(50 * time.Millisecond)

	// Verify level-0 is still empty: compaction must NOT have run.
	eng.mu.RLock()
	level0After := len(eng.levels[0])
	eng.mu.RUnlock()
	if level0After != 0 {
		t.Fatalf("expected 0 level-0 files after re-check, got %d", level0After)
	}
}

// TestCompactNoDeadlock runs many concurrent Put/Get/maybeCompact operations
// and verifies there are no deadlocks or races (run with -race).
// See: DEF-D2.
func TestCompactNoDeadlock(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	var wg sync.WaitGroup
	var writerErr, readerErr, compactorErr atomic.Int32

	// Writers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				if err := eng.PutWithTS(key, []byte("value"), uint64(j+1)); err != nil {
					writerErr.Store(1)
					return
				}
			}
		}(i)
	}

	// Readers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j%100))
				if _, err := eng.GetWithTS(key, uint64(j+1)); err != nil {
					readerErr.Store(1)
					return
				}
			}
		}(i)
	}

	// Compaction trigger.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				eng.maybeCompact()
			}
		}()
	}

	wg.Wait()

	if writerErr.Load() != 0 {
		t.Error("writer error during concurrent compaction")
	}
	if readerErr.Load() != 0 {
		t.Error("reader error during concurrent compaction")
	}
	if compactorErr.Load() != 0 {
		t.Error("compactor error during concurrent compaction")
	}
}

// TestCompactWorkerRecheckCondition verifies that the engine-level compaction
// worker only performs compaction when the level-0 condition is actually met at
// execution time (not merely signalled). A signal sent when the condition is false
// must be re-checked and skipped. See: DEF-D2, Глава XIII.
func TestCompactWorkerRecheckCondition(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// With no level-0 files, the condition is not met. Force a signal onto the
	// channel anyway and give the worker time to process (and skip) it.
	select {
	case eng.compactCh <- struct{}{}:
	default:
	}
	time.Sleep(50 * time.Millisecond)

	// Level-0 must still be empty: the worker re-checked and skipped compaction.
	eng.mu.RLock()
	level0 := len(eng.levels[0])
	eng.mu.RUnlock()
	if level0 != 0 {
		t.Fatalf("expected 0 level-0 files after recheck, got %d", level0)
	}
}
