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

//go:build stress

package tests

import (
	"context"
	"crypto/rand"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scoriadb/pkg/scoria"
)

// setupTestCFDB creates a temporary database with full CFDB interface.
func setupTestCFDB(t *testing.T) (scoria.CFDB, func()) {
	t.Helper()
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	cleanup := func() {
		db.Close()
	}
	return db, cleanup
}

// randomKey generates a random key with optional prefix.
func randomKey(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%s%x", prefix, b)
}

// randomValue generates random bytes of given size.
func randomValue(size int) []byte {
	b := make([]byte, size)
	_, _ = rand.Read(b)
	return b
}

// TestConcurrentPuts performs concurrent writes and verifies count.
func TestConcurrentPuts(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestCFDB(t)
	defer cleanup()

	const (
		numGoroutines   = 100
		opsPerGoroutine = 10_000
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < opsPerGoroutine; i++ {
				key := []byte(fmt.Sprintf("key-%d-%d", id, i))
				value := randomValue(128)
				if err := db.Put(key, value); err != nil {
					t.Errorf("goroutine %d: Put failed: %v", id, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	// Scan all keys and count.
	iter := db.Scan([]byte(""))
	defer iter.Close()
	count := 0
	for iter.Next() {
		count++
	}
	if err := iter.Err(); err != nil {
		t.Errorf("scan error: %v", err)
	}
	expected := numGoroutines * opsPerGoroutine
	if count != expected {
		t.Errorf("expected %d keys, got %d", expected, count)
	}
}

// TestConcurrentReadWrite runs readers, writers, and deleters simultaneously.
func TestConcurrentReadWrite(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestCFDB(t)
	defer cleanup()

	const (
		numReaders   = 50
		numWriters   = 50
		numDeleters  = 10
		keyPoolSize  = 1000
		duration     = 30 * time.Second
	)

	// Pre-populate key pool with initial values.
	keyPool := make([][]byte, keyPoolSize)
	valuePool := make([][]byte, keyPoolSize)
	for i := 0; i < keyPoolSize; i++ {
		keyPool[i] = []byte(fmt.Sprintf("key-%d", i))
		valuePool[i] = randomValue(64)
		if err := db.Put(keyPool[i], valuePool[i]); err != nil {
			t.Fatalf("initial put failed: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var errors uint64
	var wg sync.WaitGroup

	// Readers
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				idx := int(atomic.LoadUint64(&errors) % uint64(keyPoolSize))
				key := keyPool[idx]
				val, err := db.Get(key)
				if err != nil {
					atomic.AddUint64(&errors, 1)
					continue
				}
				_ = val // ignore value, just test that read works
				// Small sleep to prevent tight loop (optional)
				time.Sleep(time.Microsecond)
			}
		}(r)
	}

	// Writers
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				idx := int(atomic.LoadUint64(&errors) % uint64(keyPoolSize))
				key := keyPool[idx]
				newVal := []byte(fmt.Sprintf("val-%d-%d", id, time.Now().UnixNano()))
				if err := db.Put(key, newVal); err != nil {
					atomic.AddUint64(&errors, 1)
				}
				time.Sleep(time.Microsecond)
			}
		}(w)
	}

	// Deleters
	for d := 0; d < numDeleters; d++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				idx := int(atomic.LoadUint64(&errors) % uint64(keyPoolSize))
				key := keyPool[idx]
				if err := db.Delete(key); err != nil {
					atomic.AddUint64(&errors, 1)
				}
				time.Sleep(time.Microsecond * 10)
			}
		}(d)
	}

	wg.Wait()

	// Ensure database is still accessible.
	iter := db.Scan([]byte(""))
	defer iter.Close()
	scanCount := 0
	for iter.Next() {
		scanCount++
	}
	if err := iter.Err(); err != nil {
		t.Errorf("final scan error: %v", err)
	}
	t.Logf("test completed with %d errors, final key count %d", atomic.LoadUint64(&errors), scanCount)
}

// TestTransactionConflicts stresses transaction conflict detection.
func TestTransactionConflicts(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestCFDB(t)
	defer cleanup()

	const (
		numGoroutines = 20
		iterations    = 1000
		numKeys       = 10
		maxRetries    = 3
	)

	// Initialize keys
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("conflict-key-%d", i))
		if err := db.Put(key, []byte("initial")); err != nil {
			t.Fatalf("initial put failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for g := 0; g < numGoroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for it := 0; it < iterations; it++ {
				tx := db.NewTransaction()
				// Read all keys
				values := make([][]byte, numKeys)
				ok := true
				for i := 0; i < numKeys; i++ {
					key := []byte(fmt.Sprintf("conflict-key-%d", i))
					val, err := tx.Get(key)
					if err != nil {
						t.Errorf("goroutine %d iteration %d: Get failed: %v", id, it, err)
						ok = false
						break
					}
					values[i] = val
				}
				if !ok {
					tx.Rollback()
					continue
				}
				// Write new values
				for i := 0; i < numKeys; i++ {
					key := []byte(fmt.Sprintf("conflict-key-%d", i))
					newVal := []byte(fmt.Sprintf("value-%d-%d", id, it))
					if err := tx.Put(key, newVal); err != nil {
						t.Errorf("goroutine %d iteration %d: Put failed: %v", id, it, err)
						ok = false
						break
					}
				}
				if !ok {
					tx.Rollback()
					continue
				}
				// Commit with retry on conflict (error string depends on implementation)
				var err error
				for retry := 0; retry < maxRetries; retry++ {
					err = tx.Commit()
					if err == nil {
						break
					}
					// Check if conflict (wrap with error handling)
					if err.Error() == "transaction conflict" {
						// Start new transaction for retry
						tx = db.NewTransaction()
						continue
					}
					t.Errorf("goroutine %d iteration %d: Commit failed: %v", id, it, err)
					break
				}
				if err != nil {
					t.Logf("transaction aborted after %d retries", maxRetries)
				}
			}
		}(g)
	}
	wg.Wait()

	// Verify final state: all keys should exist
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("conflict-key-%d", i))
		val, err := db.Get(key)
		if err != nil {
			t.Errorf("final Get failed for key %d: %v", i, err)
		}
		if val == nil {
			t.Errorf("key %d missing after all transactions", i)
		}
	}
}

// TestLongRunningWrite writes many keys over a long period.
func TestLongRunningWrite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping long-running test in short mode")
	}
	db, cleanup := setupTestCFDB(t)
	defer cleanup()

	const (
		totalKeys     = 500_000
		valueSize     = 4 * 1024 // 4 KB (triggers VLog)
		checkInterval = 10 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
	defer cancel()

	writtenKeys := make([][]byte, 0, totalKeys)
	var writtenMu sync.RWMutex
	var lastCheck time.Time
	var memStatsStart runtime.MemStats
	runtime.ReadMemStats(&memStatsStart)

	start := time.Now()
	for i := 0; i < totalKeys; i++ {
		select {
		case <-ctx.Done():
			t.Logf("stopped after %d keys due to timeout", i)
			break
		default:
		}
		key := []byte(fmt.Sprintf("long-key-%d", i))
		value := randomValue(valueSize)
		if err := db.Put(key, value); err != nil {
			t.Fatalf("put failed at key %d: %v", i, err)
		}
		writtenMu.Lock()
		writtenKeys = append(writtenKeys, key)
		writtenMu.Unlock()

		// Periodically verify a random key
		if time.Since(lastCheck) > checkInterval {
			lastCheck = time.Now()
			writtenMu.RLock()
			if len(writtenKeys) > 0 {
				idx := i % len(writtenKeys)
				checkKey := writtenKeys[idx]
				val, err := db.Get(checkKey)
				if err != nil || val == nil {
					t.Errorf("key %x not readable after write", checkKey)
				}
			}
			writtenMu.RUnlock()
		}
		time.Sleep(1 * time.Millisecond)
	}

	t.Logf("wrote %d keys in %v", len(writtenKeys), time.Since(start))
	// Verify all keys are readable
	writtenMu.RLock()
	defer writtenMu.RUnlock()
	for _, key := range writtenKeys {
		val, err := db.Get(key)
		if err != nil || val == nil {
			t.Errorf("final verification failed for key %x", key)
			break
		}
	}
}

// TestCrashRecovery is skipped because it requires subprocess simulation.
func TestCrashRecovery(t *testing.T) {
	t.Skip("crash recovery requires subprocess testing; not implemented")
}

// TestCompactionDuringWrites writes many keys while compaction runs automatically.
func TestCompactionDuringWrites(t *testing.T) {
	t.Parallel()
	db, cleanup := setupTestCFDB(t)
	defer cleanup()

	const (
		numKeys     = 200_000
		numScanners = 5
	)

	// Write keys (this will trigger automatic compaction)
	for i := 0; i < numKeys; i++ {
		key := []byte(fmt.Sprintf("compact-key-%d", i))
		value := randomValue(64)
		if err := db.Put(key, value); err != nil {
			t.Fatalf("write failed: %v", err)
		}
	}

	// Concurrent scanners
	var wg sync.WaitGroup
	wg.Add(numScanners)
	for s := 0; s < numScanners; s++ {
		go func(id int) {
			defer wg.Done()
			iter := db.Scan([]byte(""))
			defer iter.Close()
			count := 0
			for iter.Next() {
				count++
			}
			if err := iter.Err(); err != nil {
				t.Errorf("scanner %d error: %v", id, err)
			}
			t.Logf("scanner %d saw %d keys", id, count)
		}(s)
	}
	wg.Wait()

	// Verify total unique keys
	iter := db.Scan([]byte(""))
	defer iter.Close()
	total := 0
	for iter.Next() {
		total++
	}
	if err := iter.Err(); err != nil {
		t.Errorf("final scan error: %v", err)
	}
	if total != numKeys {
		t.Errorf("expected %d keys, got %d", numKeys, total)
	}
}

// TestGCWithConcurrentWrites is skipped because no public GC API in v0.1.0.
func TestGCWithConcurrentWrites(t *testing.T) {
	t.Skip("GC not exposed in public CFDB API for v0.1.0")
}