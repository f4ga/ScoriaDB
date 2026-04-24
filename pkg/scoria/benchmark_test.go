package scoria

import (
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

// TODO: Transaction not implemented yet
// func BenchmarkScoriaTransaction(b *testing.B) {
// 	db := openScoriaBench(b)
// 	defer db.Close()
//
// 	b.ResetTimer()
// 	for i := 0; i < b.N; i++ {
// 		txn := db.NewTransaction()
// 		if err := txn.Put([]byte("txn:key"), []byte("value")); err != nil {
// 			b.Fatal(err)
// 		}
// 		if err := txn.Commit(); err != nil {
// 			b.Fatal(err)
// 		}
// 	}
// }

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