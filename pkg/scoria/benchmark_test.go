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
	"os"
	"testing"
)

func BenchmarkScoriaPut(b *testing.B) {
	db := openScoriaBench(b)
	defer db.Close()

	key := []byte("user:1")
	value := []byte("Alice")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := db.Put(key, value); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScoriaGet(b *testing.B) {
	db := openScoriaBench(b)
	defer db.Close()

	key := []byte("user:1")
	value := []byte("Alice")
	if err := db.Put(key, value); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := db.Get(key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScan(b *testing.B) {
	db := openScoriaCFBench(b)
	defer db.Close()

	// Подготовка: записываем 1000 ключей с префиксом "scan:"
	for i := 0; i < 1000; i++ {
		k := fmt.Sprintf("scan:%04d", i)
		if err := db.Put([]byte(k), []byte("val")); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it := db.Scan([]byte("scan:"))
		count := 0
		for it.Next() {
			_ = it.Key()
			_ = it.Value()
			count++
		}
		if err := it.Err(); err != nil {
			b.Fatal(err)
		}
		it.Close()
		if count != 1000 {
			b.Fatalf("expected 1000 keys, got %d", count)
		}
	}
}

func BenchmarkScoriaTransaction(b *testing.B) {
	db := openScoriaCFBench(b)
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		txn := db.NewTransaction()
		if err := txn.Put([]byte("txn:key"), []byte("value")); err != nil {
			b.Fatal(err)
		}
		if err := txn.Commit(); err != nil {
			b.Fatal(err)
		}
		// Дополнительно проверим откат
		txn2 := db.NewTransaction()
		if err := txn2.Put([]byte("txn:rollback"), []byte("val")); err != nil {
			b.Fatal(err)
		}
		if err := txn2.Rollback(); err != nil {
			b.Fatal(err)
		}
	}
}

func openScoriaBench(b *testing.B) DB {
	b.Helper()
	dir, err := os.MkdirTemp("", "scoriadb-scoria-bench")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { os.RemoveAll(dir) })

	db, err := Open(DefaultOptions(dir))
	if err != nil {
		b.Fatal(err)
	}
	return db
}

// openScoriaCFBench открывает БД и возвращает CFDB для бенчмарков,
// которым нужны расширенные методы: Scan, NewTransaction, Batch и т.д.
func openScoriaCFBench(b *testing.B) CFDB {
	b.Helper()
	dir, err := os.MkdirTemp("", "scoriadb-scoria-cfbench")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { os.RemoveAll(dir) })

	opts := DefaultOptions(dir)
	db, err := Open(opts)
	if err != nil {
		b.Fatal(err)
	}

	// Приведение к CFDB безопасно, так как *ScoriaDB реализует оба интерфейса
	cfdb, ok := db.(CFDB)
	if !ok {
		b.Fatal("returned DB does not implement CFDB")
	}
	return cfdb
}
