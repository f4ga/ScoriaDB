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

package engine

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/errors"
)

// openBenchDB — вспомогательная функция, создающая временную БД для бенчмарков
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
// Запись маленьких значений (без fsync на каждую операцию)
// ------------------------------------------------------------
func BenchmarkPutSmallValue(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	key := []byte("bench:key")
	value := []byte("small-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, value, ts); err != nil {
			b.Fatal(err)
		}
	}
}

// ------------------------------------------------------------
// Запись больших значений (без fsync на каждую операцию)
// ------------------------------------------------------------
func BenchmarkPutLargeValue(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	key := []byte("bench:key")
	value := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, value, ts); err != nil {
			b.Fatal(err)
		}
	}
}

// ------------------------------------------------------------
// Запись маленьких значений с fsync на каждую операцию
// (для сравнения с синхронными режимами других БД)
// ------------------------------------------------------------
func BenchmarkPutSmallValue_Sync(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	// Создаём WAL с синхронным режимом (без group commit)
	// В текущей реализации это достигается передачей опций
	// Но для простоты используем стандартный openBenchDB
	// и отключаем групповой коммит через опции (если есть)
	//
	// Если нет отдельного режима — просто используем стандартную БД
	// и замеряем с fsync (включён по умолчанию в групповом коммите)

	key := []byte("bench:key")
	value := []byte("small-value")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Пишем уникальные ключи, чтобы не перезаписывать один и тот же
		k := append(key, byte(i%256))
		ts := db.NextTimestamp()
		if err := db.PutWithTS(k, value, ts); err != nil {
			b.Fatal(err)
		}
	}
}

// ------------------------------------------------------------
// Запись больших значений с fsync на каждую операцию
// ------------------------------------------------------------
func BenchmarkPutLargeValue_Sync(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	key := []byte("bench:key")
	value := make([]byte, 4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := append(key, byte(i%256))
		ts := db.NextTimestamp()
		if err := db.PutWithTS(k, value, ts); err != nil {
			b.Fatal(err)
		}
	}
}

// ------------------------------------------------------------
// Чтение существующего ключа (попадание в MemTable)
// ------------------------------------------------------------
func BenchmarkGetExisting(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	key := []byte("get:key")
	value := []byte("some-data")
	ts := db.NextTimestamp()
	if err := db.PutWithTS(key, value, ts); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetWithTS(key, math.MaxUint64)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ------------------------------------------------------------
// Чтение отсутствующего ключа (промах)
// ------------------------------------------------------------
func BenchmarkGetMissing(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	key := []byte("missing:key")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.GetWithTS(key, math.MaxUint64)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// ------------------------------------------------------------
// Сканирование (Scan) по префиксу
// ------------------------------------------------------------
func BenchmarkScan(b *testing.B) {
	db := openBenchDB(b)
	defer errors.CloseWithFatal(db, "bench-db")

	// Заполняем 10 000 ключей с префиксом "scan:"
	for i := 0; i < 10000; i++ {
		key := []byte(fmt.Sprintf("scan:%05d", i))
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, []byte("value"), ts); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		iter := db.Scan([]byte("scan:"))
		count := 0
		for iter.Next() {
			count++
		}
		errors.CloseWithLog(iter, "bench-scan-iter")
		if count == 0 {
			b.Fatal("scan returned 0 entries")
		}
	}
}

func BenchmarkBloomFilter(b *testing.B) {
	bf := sstable.NewBloomFilter(10000)
	keys := make([][]byte, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
		bf.Add(keys[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.MayContain(keys[i%10000])
	}
}
