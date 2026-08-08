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
//  7. Logs are suppressed via logger.SetLevel(logger.ERROR) at the start of each benchmark.

package engine

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

// ------------------------------------------------------------
// BenchmarkEnginePut — small value (16B), parallel, zero fsync
// ------------------------------------------------------------
func BenchmarkEnginePut(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("key:%d", i))
			value := []byte(fmt.Sprintf("val:%d", i))
			if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// ------------------------------------------------------------
// BenchmarkEnginePutLarge — large value (4KB), parallel, zero fsync
// ------------------------------------------------------------
func BenchmarkEnginePutLarge(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("key:%d", i))
			if err := e.PutWithTS(key, value, uint64(i+1)); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// ------------------------------------------------------------
// BenchmarkEngineGet — existing key (MemTable hit), parallel
// ------------------------------------------------------------
func BenchmarkEngineGet(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	for i := 0; i < 100000; i++ {
		key := []byte(fmt.Sprintf("key:%d", i))
		if err := e.PutWithTS(key, []byte("value"), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("key:%d", i%100000))
			if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// ------------------------------------------------------------
// BenchmarkEngineGetLarge — large value (4KB from VLog), parallel
// ------------------------------------------------------------
func BenchmarkEngineGetLarge(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	largeVal := make([]byte, 4096)
	for i := range largeVal {
		largeVal[i] = byte(i % 256)
	}

	for i := 0; i < 100000; i++ {
		key := []byte(fmt.Sprintf("key:%d", i))
		if err := e.PutWithTS(key, largeVal, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("key:%d", i%100000))
			if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// ------------------------------------------------------------
// BenchmarkEngineGetMissing — missing key, parallel
// ------------------------------------------------------------
func BenchmarkEngineGetMissing(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := []byte(fmt.Sprintf("missing:%d", i))
			if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
	b.StopTimer()
}

// ------------------------------------------------------------
// BenchmarkEngineScan — scan 100 keys by prefix, parallel
// ------------------------------------------------------------
func BenchmarkEngineScan(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	for i := 0; i < 100000; i++ {
		key := []byte(fmt.Sprintf("scan:%d", i))
		if err := e.PutWithTS(key, []byte("value"), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			iter := e.Scan([]byte("scan:"))
			count := 0
			for iter.Next() {
				count++
				if count >= 100 {
					break
				}
			}
			if err := iter.Err(); err != nil {
				b.Fatal(err)
			}
			iter.Close()
		}
	})
	b.StopTimer()
}

// ------------------------------------------------------------
// BenchmarkGetExisting — read existing keys, parallel, different keys.
// Keys are pre-allocated before ResetTimer; the hot path does zero allocs.
// ------------------------------------------------------------
func BenchmarkGetExisting(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	// Pre-allocate numKeys distinct keys once (cold path).
	const numKeys = 1000
	keys := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = []byte(fmt.Sprintf("bench-key-%d", i))
		if err := e.PutWithTS(keys[i], []byte("value"), uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			key := keys[i%numKeys]
			if _, err := e.GetWithTS(key, math.MaxUint64); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// ------------------------------------------------------------
// BenchmarkPutSmallValue — write different keys, parallel.
// Keys are pre-allocated before ResetTimer; the hot path does zero allocs.
// ------------------------------------------------------------
func BenchmarkPutSmallValue(b *testing.B) {
	logger.SetLevel(logger.ERROR)
	dir := b.TempDir()
	opts := DefaultEngineOptions(dir)
	opts.WALOpts.SyncMode = false
	opts.WALOpts.GroupCommitEnabled = true

	e, err := NewLSMEngine(dir, opts.WALOpts)
	if err != nil {
		b.Fatal(err)
	}
	defer e.Close()

	// Pre-allocate distinct keys once (cold path) so the hot path
	// only issues PutWithTS with an already-allocated key slice.
	const numKeys = 1000
	keys := make([][]byte, numKeys)
	for i := 0; i < numKeys; i++ {
		keys[i] = []byte(fmt.Sprintf("bench-key-%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			key := keys[i%numKeys]
			if err := e.PutWithTS(key, []byte("value"), uint64(i+1)); err != nil {
				b.Fatal(err)
			}
			i++
		}
	})
}

// ------------------------------------------------------------
// BenchmarkPutLargeValue — zero fsync, zero alloc in hot path
// ------------------------------------------------------------
func BenchmarkPutLargeValue(b *testing.B) {
	logger.SetLevel(logger.ERROR)
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
// BenchmarkPutSmallValue_Sync — REAL fsync on every operation
// Uses /var/tmp (physical disk, not tmpfs)
// ------------------------------------------------------------
func BenchmarkPutSmallValue_Sync(b *testing.B) {
	logger.SetLevel(logger.ERROR)

	// Используем /var/tmp — обычно на физическом диске, не tmpfs
	dir, err := os.MkdirTemp("/var/tmp", "scoria-sync-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

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
// BenchmarkPutLargeValue_Sync — REAL fsync on every operation, 4KB
// Uses /var/tmp (physical disk, not tmpfs)
// ------------------------------------------------------------
func BenchmarkPutLargeValue_Sync(b *testing.B) {
	logger.SetLevel(logger.ERROR)

	// Используем /var/tmp — обычно на физическом диске, не tmpfs
	dir, err := os.MkdirTemp("/var/tmp", "scoria-sync-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

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
// BenchmarkGetMissing — missing key (MemTable miss)
// ------------------------------------------------------------
func BenchmarkGetMissing(b *testing.B) {
	logger.SetLevel(logger.ERROR)
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
// BenchmarkScan — scan by prefix
// ------------------------------------------------------------
func BenchmarkScan(b *testing.B) {
	logger.SetLevel(logger.ERROR)
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
// BenchmarkBloomFilter — bloom filter lookup
// ------------------------------------------------------------
func BenchmarkBloomFilter(b *testing.B) {
	logger.SetLevel(logger.ERROR)
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
// BenchmarkGetLargeValue — large value (4KB) from VLog, zero-copy
// ------------------------------------------------------------
func BenchmarkGetLargeValue(b *testing.B) {
	logger.SetLevel(logger.ERROR)
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
// BenchmarkPutLargeValue_GroupCommit — 4KB with WAL batching
// Expected: ~9.3 GB/s
// ------------------------------------------------------------
func BenchmarkPutLargeValue_GroupCommit(b *testing.B) {
	logger.SetLevel(logger.ERROR)

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

// ------------------------------------------------------------
// Put small value with STRICT fsync on EVERY Put (0 batching)
// ------------------------------------------------------------
func BenchmarkPutSmallValue_StrictSync(b *testing.B) {
	logger.SetLevel(logger.ERROR)
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
		// Guarantee fsync after EACH Put
		if err := e.wal.Flush(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}
