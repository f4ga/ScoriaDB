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

// internal/engine/benchmark_test.go
//
// Zero-Alloc Hot Path Benchmarks for v0.3.0 Arena/SkipList architecture.
//
// CRITICAL DESIGN RULES:
//  1. SyncMode=false — no fsync in benchmarks (pure in-memory LSM throughput).
//  2. GroupCommitEnabled=true — enable WAL batching.
//  3. All buffers (key, value) are allocated ONCE before b.ResetTimer().
//  4. The b.N loop contains ONLY the engine API call (PutWithTS / GetWithTS).
//  5. No fmt.Println, log.Println, time.Sleep, or defer inside the loop.
//  6. Timestamps are passed as uint64(i+1) — no NextTimestamp() call in hot path.

package engine

import (
	"math"
	"os"
	"runtime/debug"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/errors"
)

// openBenchDB creates a temporary database for benchmarks.
// NOTE: Uses default WALOptions (SyncMode=true). For zero-fsync benchmarks,
// use the per-benchmark options directly.
func openBenchDB(b *testing.B) *LSMEngine {
	b.Helper()
	dir, err := os.MkdirTemp("", "scoriadb-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { errors.RemoveAll(dir) })

	db, err := NewLSMEngine(dir)
	if err != nil {
		b.Fatal(err)
	}
	return db
}

// ------------------------------------------------------------
// Put small value — zero fsync, zero alloc in hot path
// ------------------------------------------------------------
func BenchmarkPutSmallValue(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("bench:key")
	value := []byte("small-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Put large value (4KB) — zero fsync, zero alloc in hot path
// ------------------------------------------------------------
func BenchmarkPutLargeValue(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("bench:key")
	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Put small value with fsync — for comparison with sync DBs
// ------------------------------------------------------------
func BenchmarkPutSmallValue_Sync(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = true
	opts.WALOpts.GroupCommitEnabled = false

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("bench:key")
	value := []byte("small-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Put large value (4KB) with fsync — for comparison with sync DBs
// ------------------------------------------------------------
func BenchmarkPutLargeValue_Sync(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = true
	opts.WALOpts.GroupCommitEnabled = false

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("bench:key")
	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Get existing key (MemTable hit)
// ------------------------------------------------------------
func BenchmarkGetExisting(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("get:key")
	value := []byte("some-data")
	if err := e.PutWithTS(key, value, 1); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Get missing key (MemTable miss)
// ------------------------------------------------------------
func BenchmarkGetMissing(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("missing:key")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Scan by prefix
// ------------------------------------------------------------
func BenchmarkScan(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	// Pre-populate 10,000 keys with prefix "scan:"
	for i := 0; i < 10000; i++ {
		k := []byte{'s', 'c', 'a', 'n', ':', byte(i >> 8), byte(i & 0xff)}
		if err := e.PutWithTS(k, []byte("value"), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := e.Scan([]byte("scan:"))
		count := 0
		for iter.Next() {
			count++
		}
		errors.CloseWithLog(iter, "bench-scan-iter")
		if count == 0 {
			b.Fatal("scan returned 0 entries")
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Bloom filter benchmark
// ------------------------------------------------------------
func BenchmarkBloomFilter(b *testing.B) {
	bf := sstable.NewBloomFilter(10000)
	keys := make([][]byte, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = []byte{byte(i >> 8), byte(i & 0xff)}
		bf.Add(keys[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.MayContain(keys[i%10000])
	}
}

// ------------------------------------------------------------
// Get large value (4KB) from VLog — zero-copy read path
// ------------------------------------------------------------
func BenchmarkGetLargeValue(b *testing.B) {
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("large:key")
	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}
	if err := e.PutWithTS(key, value, 1); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// ------------------------------------------------------------
// Group Commit: put large value (4KB) with WAL batching
// Expected: ~9.3 GB/s (as advertised in README)
// ------------------------------------------------------------
func BenchmarkPutLargeValue_GroupCommit(b *testing.B) {
	// Disable GC entirely for the benchmark — zero GC interference.
	gcPercent := debug.SetGCPercent(-1)
	b.Cleanup(func() { debug.SetGCPercent(gcPercent) })

	// Temporarily increase MaxInlineSize to 4096 so all values go through VLog.
	origMaxInlineSize := MaxInlineSize
	MaxInlineSize = 4096
	b.Cleanup(func() { MaxInlineSize = origMaxInlineSize })

	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true
	opts.WALOpts.GroupCommitInterval = 10 * time.Millisecond

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	key := []byte("bench:key")
	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	// Force flush remaining buffered data.
	if err := e.wal.Flush(); err != nil {
		b.Logf("flush error: %v", err)
	}
}
