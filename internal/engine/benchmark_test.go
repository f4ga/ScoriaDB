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
	"math"
	"os"
	"testing"
)

func BenchmarkPutSmallValue(b *testing.B) {
	db := openBenchDB(b)
	defer db.Close()

	key := []byte("bench:key")
	value := []byte("small-value") // < 64 байт — попадёт в LSM

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Для простоты пишем один и тот же ключ — меряем пропускную способность записи
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, value, ts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPutLargeValue(b *testing.B) {
	db := openBenchDB(b)
	defer db.Close()

	key := []byte("bench:key")
	value := make([]byte, 4096) // 4KB — пойдёт в Value Log

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := db.NextTimestamp()
		if err := db.PutWithTS(key, value, ts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetExisting(b *testing.B) {
	db := openBenchDB(b)
	defer db.Close()

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


// openBenchDB — вспомогательная функция, создающая временную БД
func openBenchDB(b *testing.B) *LSMEngine {
	b.Helper()
	dir, err := os.MkdirTemp("", "scoriadb-bench")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { os.RemoveAll(dir) })

	db, err := NewLSMEngine(dir)
	if err != nil {
		b.Fatal(err)
	}
	return db
}