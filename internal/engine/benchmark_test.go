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