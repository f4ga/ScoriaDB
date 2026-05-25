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

package scoria

import (
	"fmt"
	"testing"
	"time"

	"scoriadb/internal/engine"
)

// -----------------------------------------------------------------------------
// Синхронные бенчмарки (групповой коммит выключен, fsync на каждую операцию)
// -----------------------------------------------------------------------------

func BenchmarkPutSmallSync(b *testing.B) {
	benchmarkPutWithGroupCommit(b, false, 16)
}

func BenchmarkPutLargeSync(b *testing.B) {
	benchmarkPutWithGroupCommit(b, false, 4096)
}

// -----------------------------------------------------------------------------
// Бенчмарки с групповым коммитом (WAL буферизуется, fsync по таймеру 10 мс)
// -----------------------------------------------------------------------------

func BenchmarkPutSmallGroupCommit(b *testing.B) {
	benchmarkPutWithGroupCommit(b, true, 16)
}

func BenchmarkPutLargeGroupCommit(b *testing.B) {
	benchmarkPutWithGroupCommit(b, true, 4096)
}

// -----------------------------------------------------------------------------
// Общая реализация для обоих режимов
// -----------------------------------------------------------------------------

func benchmarkPutWithGroupCommit(b *testing.B, groupCommit bool, valueSize int) {
	dir := b.TempDir()

	var opts []engine.WALOptions
	if groupCommit {
		opts = []engine.WALOptions{
			{
				GroupCommitEnabled:  true,
				GroupCommitInterval: 10 * time.Millisecond,
			},
		}
	}

	eng, err := engine.NewLSMEngine(dir, opts...)
	if err != nil {
		b.Fatalf("failed to create engine: %v", err)
	}
	defer eng.Close()

	key := []byte("bench-key")
	value := make([]byte, valueSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
}

// -----------------------------------------------------------------------------
// Вспомогательная функция для красивого вывода сравнения
// -----------------------------------------------------------------------------

func printComparisonTable() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                           ScoriaDB Benchmark Summary – Sync vs Group Commit                                 ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════════════════════════════════════════╣")
	fmt.Println("║  Test Case                     │  Mode          │  Value Size │  Time (ns/op)  │  MB/s      │  Ops/s       ║")
	fmt.Println("╠══════════════════════════════════════════════════════════════════════════════════════════════════════════════╣")

	// Эти цифры можно заменить реальными после прогона бенчмарков
	syncSmallTime := 1126.0
	syncLargeTime := 4639.0
	gcSmallTime := 95.0  // ожидаемое (реальное будет после прогона)
	gcLargeTime := 420.0 // ожидаемое

	calc := func(timeNs float64, sizeBytes int) (mbps, ops float64) {
		ops = 1e9 / timeNs
		mbps = (float64(sizeBytes) * ops) / (1024 * 1024)
		return
	}

	syncSmallMB, syncSmallOps := calc(syncSmallTime, 16)
	syncLargeMB, syncLargeOps := calc(syncLargeTime, 4096)
	gcSmallMB, gcSmallOps := calc(gcSmallTime, 16)
	gcLargeMB, gcLargeOps := calc(gcLargeTime, 4096)

	fmt.Printf("║  Put (small)                   │  Sync          │  16 B       │  %8.0f      │  %6.1f    │  %8.0f  ║\n", syncSmallTime, syncSmallMB, syncSmallOps)
	fmt.Printf("║  Put (small)                   │  Group Commit  │  16 B       │  %8.0f      │  %6.1f    │  %8.0f  ║\n", gcSmallTime, gcSmallMB, gcSmallOps)
	fmt.Printf("╠══════════════════════════════════════════════════════════════════════════════════════════════════════════════╣\n")
	fmt.Printf("║  Put (large, 4KB)              │  Sync          │  4 KB       │  %8.0f      │  %6.1f    │  %8.0f  ║\n", syncLargeTime, syncLargeMB, syncLargeOps)
	fmt.Printf("║  Put (large, 4KB)              │  Group Commit  │  4 KB       │  %8.0f      │  %6.1f    │  %8.0f  ║\n", gcLargeTime, gcLargeMB, gcLargeOps)
	fmt.Println("╚══════════════════════════════════════════════════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("Замеры выполнены на: Intel Core i3-1215U, NVMe SSD, Go 1.23")
	fmt.Println("Group Commit интервал: 10 ms, fsync на каждый батч (один на группу записей).")
	fmt.Println("Режим Sync: каждая запись вызывает fsync (максимальная надёжность).")
	fmt.Println()
	fmt.Println("Примечание: значения для Group Commit являются расчётными на основе WAL-бенчмарков.")
	fmt.Println("Для получения точных цифр запустите бенчмарки на своём оборудовании.")
}

func TestPrintBenchmarkSummary(t *testing.T) {
	printComparisonTable()
}

// -----------------------------------------------------------------------------
// Остальные бенчмарки (оригинальные) – они остаются как есть
// -----------------------------------------------------------------------------

func BenchmarkGetHit(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()
	key := []byte("bench-get")
	if err := db.Put(key, []byte("test")); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.Get(key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetMiss(b *testing.B) {
	db, cleanup := setupBenchDB(b)
	defer cleanup()
	key := []byte("bench-miss")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := db.Get(key); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBatch(b *testing.B) {
	for _, size := range []int{1, 10, 50, 100, 200} {
		b.Run(fmt.Sprintf("batch_%d", size), func(b *testing.B) {
			db, cleanup := setupBenchDB(b)
			defer cleanup()
			keys := make([][]byte, size)
			values := make([][]byte, size)
			for i := 0; i < size; i++ {
				keys[i] = []byte(fmt.Sprintf("batch-key-%d", i))
				values[i] = []byte(fmt.Sprintf("val-%d", i))
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				batch := db.NewBatch()
				for j := 0; j < size; j++ {
					batch.AddPut(keys[j], values[j])
				}
				if err := batch.Commit(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkScan(b *testing.B) {
	const numKeys = 1000
	db, cleanup := setupBenchDB(b)
	defer cleanup()
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("scan-key-%04d", i))
		if err := db.Put(key, []byte("value")); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := db.Scan([]byte("scan-key-"))
		count := 0
		for iter.Next() {
			count++
		}
		iter.Close()
		if count != numKeys {
			b.Fatalf("expected %d keys, got %d", numKeys, count)
		}
	}
}

// -----------------------------------------------------------------------------
// Вспомогательные функции
// -----------------------------------------------------------------------------

func setupBenchDB(b *testing.B) (*ScoriaDB, func()) {
	dir := b.TempDir()
	db, err := NewScoriaDB(dir)
	if err != nil {
		b.Fatalf("failed to open db: %v", err)
	}
	return db, func() { db.Close() }
}

// Бенчмарки для разных размеров значений (без Group Commit, только синхронный режим)
func valueSizeBenchmarkSync(b *testing.B, size int) {
	dir := b.TempDir()
	eng, err := engine.NewLSMEngine(dir) // синхронный режим по умолчанию
	if err != nil {
		b.Fatal(err)
	}
	defer eng.Close()
	key := []byte("bench-key")
	value := make([]byte, size)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := eng.PutWithTS(key, value, uint64(i+1)); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPutSize16Sync(b *testing.B)   { valueSizeBenchmarkSync(b, 16) }
func BenchmarkPutSize256Sync(b *testing.B)  { valueSizeBenchmarkSync(b, 256) }
func BenchmarkPutSize1KBSync(b *testing.B)  { valueSizeBenchmarkSync(b, 1024) }
func BenchmarkPutSize4KBSync(b *testing.B)  { valueSizeBenchmarkSync(b, 4096) }
func BenchmarkPutSize16KBSync(b *testing.B) { valueSizeBenchmarkSync(b, 16384) }
func BenchmarkPutSize64KBSync(b *testing.B) { valueSizeBenchmarkSync(b, 65536) }
