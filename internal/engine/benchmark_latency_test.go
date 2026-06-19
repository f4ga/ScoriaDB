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
	"math"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
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
// cleanTemp — очистка временных файлов
// ---------------------------------------------------------------------------

func cleanTemp() {
	os.RemoveAll("/tmp/scoriadb-*")
	os.RemoveAll("/tmp/TestVLogRecoveryAfterCrash*")
	os.RemoveAll("/tmp/scoriadb-latency-bench-*")
	os.RemoveAll("/tmp/scoria-*")
}

// ---------------------------------------------------------------------------
// openTestDBWithOptions
// ---------------------------------------------------------------------------

func openTestDBWithOptions(b *testing.B, walOpts WALOptions) *LSMEngine {
	b.Helper()

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
			os.RemoveAll(dir)
			cleanTemp()
		}
	}()

	db, err := NewLSMEngine(dir, walOpts)
	if err != nil {
		os.RemoveAll(dir)
		b.Fatal(err)
	}
	return db
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_Sync
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_Sync(b *testing.B) {
	opts := DefaultWALOptions()
	opts.GroupCommitEnabled = false

	db := openTestDBWithOptions(b, opts)
	defer db.Close()

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		key := []byte("bench:key")
		value := []byte("value")

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
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer db.Close()

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		key := []byte("bench:key")
		value := []byte("value")

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
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 1 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer db.Close()

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		key := []byte("bench:key")
		value := []byte("value")

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
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer db.Close()

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		localCounter := uint64(0)
		value := []byte("value")

		for pb.Next() {
			localCounter++
			key := []byte(fmt.Sprintf("bench:key:%d", localCounter))

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

	collector.report(b, "Put Latency (Group Commit, varied keys)")
}

// ---------------------------------------------------------------------------
// BenchmarkPutLatency_GroupCommit_4KB
// ---------------------------------------------------------------------------

func BenchmarkPutLatency_GroupCommit_4KB(b *testing.B) {
	opts := DefaultWALOptions()
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	db := openTestDBWithOptions(b, opts)
	defer db.Close()

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		key := []byte("bench:key")
		value := make([]byte, 4096)

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
	db := openBenchDB(b)
	defer db.Close()

	key := []byte("bench:key")
	value := []byte("value")
	ts := db.NextTimestamp()
	if err := db.PutWithTS(key, value, ts); err != nil {
		b.Fatal(err)
	}

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			start := time.Now()
			_, err := db.GetWithTS(key, math.MaxUint64)
			if err != nil {
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
	db := openBenchDB(b)
	defer db.Close()

	key := []byte("missing:key")
	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		for pb.Next() {
			start := time.Now()
			_, err := db.GetWithTS(key, math.MaxUint64)
			if err != nil {
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
	db := openBenchDB(b)
	defer db.Close()

	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("scan:%05d", i))
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, []byte("value"), ts); err != nil {
			b.Fatal(err)
		}
	}

	collector := &latencyCollector{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		start := time.Now()
		iter := db.Scan([]byte("scan:"))
		count := 0
		for iter.Next() {
			count++
		}
		iter.Close()
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
	db := openBenchDB(b)
	defer db.Close()

	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("read:%05d", i))
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, []byte("value"), ts); err != nil {
			b.Fatal(err)
		}
	}

	collector := &latencyCollector{}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		localCollector := &latencyCollector{}
		localCounter := uint64(0)
		value := []byte("value")

		for pb.Next() {
			localCounter++
			start := time.Now()

			if localCounter%5 == 0 {
				key := []byte(fmt.Sprintf("write:%d", localCounter))
				ts := db.NextTimestamp()
				if err := db.PutWithTS(key, value, ts); err != nil {
					b.Error(err)
					return
				}
			} else {
				key := []byte(fmt.Sprintf("read:%05d", localCounter%10000))
				if _, err := db.GetWithTS(key, math.MaxUint64); err != nil {
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
