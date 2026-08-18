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
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

// TestFlushNonBlocking verifies that Put operations are NOT blocked while a flush
// is in progress. The new flushMemTable writes SSTables WITHOUT holding e.mu.Lock,
// so concurrent writers continue uninterrupted. See: LSM-02, Глава XIII.
func TestFlushNonBlocking(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Populate the MemTable so the flush has data to write.
	for i := 0; i < 64; i++ {
		key := []byte(fmt.Sprintf("fk-%d", i))
		if err := eng.PutWithTS(key, []byte(fmt.Sprintf("v-%d", i)), uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	// Start a flush in the background.
	flushDone := make(chan error, 1)
	go func() {
		flushDone <- eng.flushMemTable()
	}()

	// While the flush is running, perform concurrent Put operations. They must
	// NOT block. The per-shard mutex is short; the engine lock is not held during
	// SSTable writing.
	var wg sync.WaitGroup
	putErr := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				key := []byte(fmt.Sprintf("live-%d-%d", id, j))
				if err := eng.PutWithTS(key, []byte("live"), uint64(j+1)); err != nil {
					putErr <- err
					return
				}
			}
		}(i)
	}
	wg.Wait()
	close(putErr)
	for err := range putErr {
		t.Errorf("Put during flush failed: %v", err)
	}

	if err := <-flushDone; err != nil {
		t.Fatalf("flushMemTable failed: %v", err)
	}
}

// TestFlushParallelShards verifies that all eligible shards are flushed in a
// single pass (their MemTables are swapped and each is written to its own SSTable).
// See: LSM-02.
func TestFlushParallelShards(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Write to every shard so each has a non-empty MemTable.
	for i := 0; i < 256; i++ {
		key := []byte(fmt.Sprintf("ps-%d", i))
		if err := eng.PutWithTS(key, []byte(fmt.Sprintf("v-%d", i)), uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	// Confirm every shard has data.
	for idx, shard := range eng.shards {
		if shard.memSizeLoad() == 0 {
			t.Fatalf("shard %d has no data to flush", idx)
		}
	}

	if err := eng.flushMemTable(); err != nil {
		t.Fatalf("flushMemTable failed: %v", err)
	}

	// Every shard must now have an empty active MemTable (data moved to frozen /
	// flushed into SSTables).
	for idx, shard := range eng.shards {
		if shard.memSizeLoad() != 0 {
			t.Errorf("shard %d memSize not reset after flush: %d", idx, shard.memSizeLoad())
		}
	}
}

// TestFlushConsistency verifies that after flushing N keys, all N are still
// readable (either from the frozen MemTable during flush or from the published
// SSTable after flush completes). See: LSM-02.
func TestFlushConsistency(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	const n = 128
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("cons-%d", i))
		if err := eng.PutWithTS(key, []byte(fmt.Sprintf("val-%d", i)), uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	if err := eng.flushMemTable(); err != nil {
		t.Fatalf("flushMemTable failed: %v", err)
	}

	// Verify all N keys are still readable.
	for i := 0; i < n; i++ {
		key := []byte(fmt.Sprintf("cons-%d", i))
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS failed for %s: %v", key, err)
		}
		want := []byte(fmt.Sprintf("val-%d", i))
		if !bytes.Equal(got, want) {
			t.Errorf("key %s: expected %q, got %q", key, want, got)
		}
	}
}

// BenchmarkFlushWhileWriting measures the impact of concurrent flush on write
// latency. See: LSM-02.
func BenchmarkFlushWhileWriting(b *testing.B) {
	dir := b.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		b.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	// Pre-populate so flush has data.
	for i := 0; i < 32; i++ {
		if err := eng.PutWithTS([]byte(fmt.Sprintf("seed-%d", i)), []byte("v"), uint64(i+1)); err != nil {
			b.Fatalf("seed PutWithTS: %v", err)
		}
	}

	// Flush periodically in the background.
	stopFlush := make(chan struct{})
	var flushWg sync.WaitGroup
	flushWg.Add(1)
	go func() {
		defer flushWg.Done()
		for {
			select {
			case <-stopFlush:
				return
			case <-time.After(5 * time.Millisecond):
				_ = eng.flushMemTable()
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := []byte(fmt.Sprintf("b-%d", i%1000))
		if err := eng.PutWithTS(key, []byte("value"), uint64(i+1)); err != nil {
			b.Fatalf("PutWithTS failed: %v", err)
		}
	}
	b.StopTimer()
	close(stopFlush)
	flushWg.Wait()
}

// TestFlushRace runs many concurrent Put/Get/flushMemTable operations and
// verifies there are no data races (run with -race). See: LSM-02.
func TestFlushRace(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	var wg sync.WaitGroup
	var putErr, getErr, flushErr atomic.Int32

	// Writers.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, j))
				if err := eng.PutWithTS(key, []byte("value"), uint64(j+1)); err != nil {
					putErr.Store(1)
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
					getErr.Store(1)
					return
				}
			}
		}(i)
	}

	// Flusher.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := 0; j < 50; j++ {
			if err := eng.flushMemTable(); err != nil {
				flushErr.Store(1)
				return
			}
		}
	}()

	wg.Wait()

	if putErr.Load() != 0 {
		t.Error("put error during concurrent flush")
	}
	if getErr.Load() != 0 {
		t.Error("get error during concurrent flush")
	}
	if flushErr.Load() != 0 {
		t.Error("flush error during concurrent flush")
	}
}
