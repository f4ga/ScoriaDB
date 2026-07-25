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

// internal/engine/benchmark_latency_test.go
//
// Latency benchmarks for v0.3.0 Arena/SkipList architecture.
//
// CRITICAL DESIGN RULES:
//  1. SyncMode=false — no fsync in benchmarks (pure in-memory LSM latency).
//  2. GroupCommitEnabled=true — enable WAL batching.
//  3. All buffers (key, value) are allocated ONCE before b.ResetTimer().
//  4. No fmt.Sprintf, append, or key allocation inside the hot loop.
//  5. Timestamps are passed as uint64(i+1) — no NextTimestamp() call in hot path.

package engine

import (
	"math"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

// ---------------------------------------------------------------------------
// latencyCollector
// ---------------------------------------------------------------------------

type latencyCollector struct {
	mu   sync.Mutex
	data []time.Duration
}

func (c *latencyCollector) Add(d time.Duration) {
	c.mu.Lock()
	c.data = append(c.data, d)
	c.mu.Unlock()
}

func (c *latencyCollector) Percentile(p float64) time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.data) == 0 {
		return 0
	}
	sorted := make([]time.Duration, len(c.data))
	copy(sorted, c.data)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(float64(len(sorted)) * p)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (c *latencyCollector) report(b *testing.B, label string) {
	b.Helper()
	b.Logf("\n=== %s ===\n", label)
	b.Logf("  Throughput: %.0f ops/s", float64(b.N)/b.Elapsed().Seconds())
	b.Logf("  p50:  %v", c.Percentile(0.50))
	b.Logf("  p95:  %v", c.Percentile(0.95))
	b.Logf("  p99:  %v", c.Percentile(0.99))
	b.Logf("  p999: %v", c.Percentile(0.999))
}

// ---------------------------------------------------------------------------
// cleanTemp — cleanup temporary files
// ---------------------------------------------------------------------------

func cleanTemp() {
	errors.RemoveAll("/tmp/scoriadb-*")
	errors.RemoveAll("/tmp/TestVLogRecoveryAfterCrash*")
	errors.RemoveAll("/tmp/scoriadb-latency-bench-*")
	errors.RemoveAll("/tmp/scoria-*")
}

// ---------------------------------------------------------------------------
// openTestDBWithOptions
// ---------------------------------------------------------------------------

func openTestDBWithOptions(b *testing.B, walOpts WALOptions) *LSMEngine {
	b.Helper()
	logger.SetLevel(logger.ERROR)

	cleanTemp()

	dir, err := os.MkdirTemp("", "scoriadb-latency-bench-*")
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			b.Logf("WARNING: failed to remove %s: %v", dir, err)
		}
		cleanTemp()
	})

	defer func() {
		if b.Failed() {
			errors.RemoveAll(dir)
			cleanTemp()
		}
	}()

	db, err := NewLSMEngine(dir, walOpts)
	if err != nil {
		errors.RemoveAll(dir)
		b.Fatal(err)
	}
	return db
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_Sync
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_Sync(b *testing.B) {
	opts := DefaultWALOptions()
	opts.SyncMode = true
	opts.GroupCommitEnabled = false

	db := openTestDBWithOptions(b, opts)
	defer errors.CloseWithFatal(db, "bench-db")

	collector := &latencyCollector{}
	key := []byte("bench:key")
	value := []byte("value")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			ts := db.NextTimestamp()
			start := time.Now()
			if err := db.PutWithTS(key, value, ts); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Put Latency (Sync, no group commit)")
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_GroupCommit
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_GroupCommit(b *testing.B) {
	opts := DefaultWALOptions()
	opts.SyncMode = false
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer errors.CloseWithFatal(db, "bench-db")

	collector := &latencyCollector{}
	key := []byte("bench:key")
	value := []byte("value")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			ts := db.NextTimestamp()
			start := time.Now()
			if err := db.PutWithTS(key, value, ts); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Put Latency (Group Commit, 10ms)")
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_GroupCommit_ShortInterval
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_GroupCommit_ShortInterval(b *testing.B) {
	opts := DefaultWALOptions()
	opts.SyncMode = false
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 1 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer errors.CloseWithFatal(db, "bench-db")

	collector := &latencyCollector{}
	key := []byte("bench:key")
	value := []byte("value")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			ts := db.NextTimestamp()
			start := time.Now()
			if err := db.PutWithTS(key, value, ts); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Put Latency (Group Commit, 1ms)")
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_GroupCommit_Varied
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_GroupCommit_Varied(b *testing.B) {
	opts := DefaultWALOptions()
	opts.SyncMode = false
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer errors.CloseWithFatal(db, "bench-db")

	collector := &latencyCollector{}
	value := []byte("value")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		localCounter := uint64(0)
		// Pre-allocate key buffer with capacity for "bench:key:" + max digits
		keyPrefix := []byte("bench:key:")
		keyBuf := make([]byte, len(keyPrefix)+20)

		for pb.Next() {
			localCounter++
			// Build key without fmt.Sprintf — manual byte copy
			n := len(keyPrefix)
			copy(keyBuf, keyPrefix)
			// Format localCounter as decimal into keyBuf[n:]
			tmp := localCounter
			pos := n + 19
			for tmp >= 10 {
				pos--
				keyBuf[pos] = byte('0' + tmp%10)
				tmp /= 10
			}
			pos--
			keyBuf[pos] = byte('0' + tmp)
			key := keyBuf[pos:]

			start := time.Now()
			if err := db.PutWithTS(key, value, localCounter); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Put Latency (Group Commit, varied keys)")
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_GroupCommit_4KB
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_GroupCommit_4KB(b *testing.B) {
	opts := DefaultWALOptions()
	opts.SyncMode = false
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer errors.CloseWithFatal(db, "bench-db")

	collector := &latencyCollector{}
	key := []byte("bench:key")
	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			ts := db.NextTimestamp()
			start := time.Now()
			if err := db.PutWithTS(key, value, ts); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Put Latency (4KB value)")
}

// ---------------------------------------------------------------------------
// BenchmarkGetLatency
// ---------------------------------------------------------------------------

func BenchmarkGetLatency(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer errors.CloseWithFatal(e, "bench-db")

	key := []byte("bench:key")
	value := []byte("value")
	if err := e.PutWithTS(key, value, 1); err != nil {
		b.Fatal(err)
	}

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			start := time.Now()
			if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Get Latency (MemTable hit)")
}

// ---------------------------------------------------------------------------
// BenchmarkGetLatency_Missing
// ---------------------------------------------------------------------------

func BenchmarkGetLatency_Missing(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer errors.CloseWithFatal(e, "bench-db")

	key := []byte("missing:key")
	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			start := time.Now()
			if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
				b.Error(err)
				return
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Get Latency (Missing key)")
}

// ---------------------------------------------------------------------------
// BenchmarkScanLatency
// ---------------------------------------------------------------------------

func BenchmarkScanLatency(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer errors.CloseWithFatal(e, "bench-db")

	// Pre-populate 10,000 keys with prefix "scan:"
	for i := 0; i < 10000; i++ {
		k := []byte{'s', 'c', 'a', 'n', ':', byte(i >> 8), byte(i & 0xff)}
		if err := e.PutWithTS(k, []byte("value"), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	collector := &latencyCollector{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		iter := e.Scan([]byte("scan:"))
		count := 0
		for iter.Next() {
			count++
		}
		errors.CloseWithLog(iter, "bench-scan-iter")
		collector.Add(time.Since(start))
		if count == 0 {
			b.Fatal("scan returned 0 entries")
		}
	}

	collector.report(b, "Scan Latency (10k keys)")
}

// ---------------------------------------------------------------------------
// BenchmarkMixedWorkload
// ---------------------------------------------------------------------------

func BenchmarkMixedWorkload(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer errors.CloseWithFatal(e, "bench-db")

	// Pre-populate 10,000 keys with prefix "read:"
	for i := 0; i < 10000; i++ {
		k := []byte{'r', 'e', 'a', 'd', ':', byte(i >> 8), byte(i & 0xff)}
		if err := e.PutWithTS(k, []byte("value"), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		localCounter := uint64(0)
		value := []byte("value")
		// Pre-allocate key buffer for write keys
		writeKeyPrefix := []byte("write:")
		writeKeyBuf := make([]byte, len(writeKeyPrefix)+20)
		// Pre-allocate key buffer for read keys
		readKeyBuf := make([]byte, 7) // "read:XX" max

		for pb.Next() {
			localCounter++
			start := time.Now()

			if localCounter%5 == 0 {
				// Write path — build key without fmt.Sprintf
				n := len(writeKeyPrefix)
				copy(writeKeyBuf, writeKeyPrefix)
				tmp := localCounter
				pos := n + 19
				for tmp >= 10 {
					pos--
					writeKeyBuf[pos] = byte('0' + tmp%10)
					tmp /= 10
				}
				pos--
				writeKeyBuf[pos] = byte('0' + tmp)
				key := writeKeyBuf[pos:]

				if err := e.PutWithTS(key, value, localCounter); err != nil {
					b.Error(err)
					return
				}
			} else {
				// Read path — build key without fmt.Sprintf
				idx := localCounter % 10000
				readKeyBuf[0] = 'r'
				readKeyBuf[1] = 'e'
				readKeyBuf[2] = 'a'
				readKeyBuf[3] = 'd'
				readKeyBuf[4] = ':'
				readKeyBuf[5] = byte(idx >> 8)
				readKeyBuf[6] = byte(idx & 0xff)
				key := readKeyBuf[:7]

				if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
					b.Error(err)
					return
				}
			}
			localCollector.Add(time.Since(start))
		}

		collector.mu.Lock()
		collector.data = append(collector.data, localCollector.data...)
		collector.mu.Unlock()
	})

	collector.report(b, "Mixed Workload (80% Read / 20% Write)")
}
