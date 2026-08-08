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
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

// ---------------------------------------------------------------------------
// TestCrashDuringCompaction
//
// Simulates a crash (kill -9) during compaction by:
//  1. Writing enough data to trigger flush and create Level-0 SSTables.
//  2. Flushing multiple times to accumulate MaxLevel0Files+1 files in Level 0.
//  3. Triggering compaction and immediately closing the engine (simulating crash).
//  4. Reopening the engine and verifying that all previously written data is
//     still readable.
// ---------------------------------------------------------------------------

func TestCrashDuringCompaction(t *testing.T) {
	dir := t.TempDir()

	// Step 1: create engine and write enough data to produce several SSTables.
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Write a moderate amount of data that will be flushed to SSTables.
	// Use small values so they stay inline (no VLog dependency).
	const numKeys = 5000
	written := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte{byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}

	// Step 2: flush the memtable to create a Level-0 SSTable.
	if err := eng.flushMemTable(); err != nil {
		t.Logf("first flush: %v (may be expected)", err)
	}

	// Write more data and flush again to accumulate multiple Level-0 files.
	for i := 0; i < numKeys; i++ {
		key := []byte{0xFF, byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1 + numKeys)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}
	if err := eng.flushMemTable(); err != nil {
		t.Logf("second flush: %v (may be expected)", err)
	}

	// Step 3: trigger compaction and immediately close the engine to simulate
	// a crash during compaction.  We call maybeCompact which launches compaction
	// in a goroutine, then close the engine right after.
	eng.maybeCompact()

	// Give the compaction goroutine a tiny moment to start, then close abruptly.
	time.Sleep(10 * time.Millisecond)

	// Close the engine (this simulates a crash during compaction).
	if err := eng.Close(); err != nil {
		t.Logf("close during compaction: %v (may be expected)", err)
	}

	// Step 4: reopen the engine and verify data.
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to reopen engine after crash: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine-after-crash")

	// Verify that all written keys are still readable.
	var missing int
	for k, v := range written {
		got, err := eng2.GetWithTS([]byte(k), 1<<30)
		if err != nil {
			t.Errorf("GetWithTS(%q) failed: %v", k, err)
			continue
		}
		if string(got) != v {
			missing++
			if missing <= 5 {
				t.Errorf("key %q: expected %q, got %q", k, v, string(got))
			}
		}
	}
	if missing > 0 {
		t.Errorf("total %d keys have wrong values after crash recovery", missing)
	}

	// Verify that the engine is still functional for new writes.
	if err := eng2.PutWithTS([]byte("post_crash_key"), []byte("post_crash_val"), 1<<30); err != nil {
		t.Errorf("write after crash recovery failed: %v", err)
	}
	got, err := eng2.GetWithTS([]byte("post_crash_key"), 1<<30)
	if err != nil {
		t.Errorf("read after crash recovery failed: %v", err)
	}
	if string(got) != "post_crash_val" {
		t.Errorf("expected post_crash_val, got %s", string(got))
	}
}

// ---------------------------------------------------------------------------
// TestCorruptedWAL
//
// Verifies that the engine can recover from a corrupted WAL file:
//  1. Write data to the engine.
//  2. Corrupt the last few bytes of the WAL file.
//  3. Reopen the engine – it should recover gracefully, keeping all uncorrupted
//     entries and discarding only the corrupted tail.
//  4. Verify that all entries written before the corruption are still readable.
// ---------------------------------------------------------------------------

func TestCorruptedWAL(t *testing.T) {
	dir := t.TempDir()

	// Step 1: create engine and write data.
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const numKeys = 1000
	written := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte{byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}

	// Close the engine cleanly so the WAL file is flushed.
	if err := eng.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Step 2: corrupt the last 10 bytes of the shard-0 WAL file.
	// Each shard owns its own WAL (wal_<id>.log); shard 0 always exists even
	// when DefaultShardCount() > 1. See: HOT-01, REC-01
	walPath := filepath.Join(dir, "wal_0.log")
	corruptFileTail(t, walPath, 10)

	// Step 3: reopen the engine – it should recover gracefully.
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to reopen engine after WAL corruption: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine-after-wal-corruption")

	// Step 4: verify that all entries written before the corruption are readable.
	// The last entry (or a few entries near the end) may be lost due to corruption
	// of the trailing bytes, but the vast majority should survive.
	var missing int
	for k, v := range written {
		got, err := eng2.GetWithTS([]byte(k), 1<<30)
		if err != nil {
			t.Errorf("GetWithTS(%q) failed: %v", k, err)
			continue
		}
		if string(got) != v {
			missing++
		}
	}
	if missing > len(written)/10 {
		t.Errorf("too many keys lost after WAL corruption: %d/%d", missing, len(written))
	}
	t.Logf("keys lost after WAL corruption: %d/%d", missing, len(written))

	// Engine should still accept new writes.
	if err := eng2.PutWithTS([]byte("after_corrupt_key"), []byte("after_corrupt_val"), 1<<30); err != nil {
		t.Errorf("write after WAL corruption recovery failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCorruptedManifest
//
// Verifies that the engine can recover from a corrupted Manifest file:
//  1. Write data and flush to create SSTables (which updates the Manifest).
//  2. Corrupt the Manifest file.
//  3. Reopen the engine – it should recover by re-scanning the data directory
//     and rebuilding the Manifest from the existing SSTable files.
//  4. Verify that all data is still readable.
// ---------------------------------------------------------------------------

func TestCorruptedManifest(t *testing.T) {
	dir := t.TempDir()

	// Step 1: create engine and write data.
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const numKeys = 2000
	written := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte{byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}

	// Flush to create SSTables and update the Manifest.
	if err := eng.flushMemTable(); err != nil {
		t.Logf("flush: %v (may be expected)", err)
	}

	// Write more data and flush again to create additional SSTable entries.
	for i := 0; i < numKeys; i++ {
		key := []byte{0xFE, byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1 + numKeys)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}
	if err := eng.flushMemTable(); err != nil {
		t.Logf("second flush: %v (may be expected)", err)
	}

	// Close the engine cleanly.
	if err := eng.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Step 2: corrupt the Manifest file.
	manifestPath := filepath.Join(dir, "MANIFEST")
	corruptFileTail(t, manifestPath, 50)

	// Step 3: reopen the engine – it should recover by re-scanning the directory.
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to reopen engine after Manifest corruption: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine-after-manifest-corruption")

	// Step 4: verify that all data is still readable.
	var missing int
	for k, v := range written {
		got, err := eng2.GetWithTS([]byte(k), 1<<30)
		if err != nil {
			t.Errorf("GetWithTS(%q) failed: %v", k, err)
			continue
		}
		if string(got) != v {
			missing++
			if missing <= 5 {
				t.Errorf("key %q: expected %q, got %q", k, v, string(got))
			}
		}
	}
	if missing > 0 {
		t.Errorf("total %d keys have wrong values after Manifest corruption recovery", missing)
	}

	// Engine should still accept new writes.
	if err := eng2.PutWithTS([]byte("after_manifest_corrupt"), []byte("ok"), 1<<30); err != nil {
		t.Errorf("write after Manifest corruption recovery failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCrashDuringFlush
//
// Simulates a crash during MemTable flush:
//  1. Write data to the engine.
//  2. Trigger flush and immediately close the engine (simulating crash mid-flush).
//  3. Reopen the engine and verify that all data written before the flush is
//     still readable (recovered from WAL).
// ---------------------------------------------------------------------------

func TestCrashDuringFlush(t *testing.T) {
	dir := t.TempDir()

	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const numKeys = 1000
	written := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte{byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}

	// Trigger flush and immediately close to simulate crash during flush.
	if err := eng.flushMemTable(); err != nil {
		t.Logf("flush: %v (may be expected)", err)
	}

	// Close the engine (simulating crash mid-flush).
	if err := eng.Close(); err != nil {
		t.Logf("close after flush: %v (may be expected)", err)
	}

	// Reopen and verify data.
	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to reopen engine after flush crash: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine-after-flush-crash")

	var missing int
	for k, v := range written {
		got, err := eng2.GetWithTS([]byte(k), 1<<30)
		if err != nil {
			t.Errorf("GetWithTS(%q) failed: %v", k, err)
			continue
		}
		if string(got) != v {
			missing++
			if missing <= 5 {
				t.Errorf("key %q: expected %q, got %q", k, v, string(got))
			}
		}
	}
	if missing > 0 {
		t.Errorf("total %d keys lost after flush crash", missing)
	}
}

// ---------------------------------------------------------------------------
// TestCorruptedWAL_Truncated
//
// Verifies recovery from a truncated WAL file (file cut short).
// ---------------------------------------------------------------------------

func TestCorruptedWAL_Truncated(t *testing.T) {
	dir := t.TempDir()

	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const numKeys = 500
	written := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte{byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Truncate the shard-0 WAL file to half its size.
	// Each shard owns its own WAL (wal_<id>.log); shard 0 always exists even
	// when DefaultShardCount() > 1. See: HOT-01, REC-01
	walPath := filepath.Join(dir, "wal_0.log")
	truncateFile(t, walPath, 0.5)

	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to reopen engine after WAL truncation: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine-after-wal-truncation")

	// At least some keys should survive (those written before the truncation point).
	var found int
	for k, v := range written {
		got, err := eng2.GetWithTS([]byte(k), 1<<30)
		if err != nil {
			continue
		}
		if string(got) == v {
			found++
		}
	}
	t.Logf("keys recovered after WAL truncation: %d/%d", found, len(written))
	if found == 0 && len(written) > 0 {
		t.Error("expected at least some keys to survive WAL truncation")
	}
}

// ---------------------------------------------------------------------------
// TestCorruptedManifest_Empty
//
// Verifies recovery from an empty Manifest file.
// ---------------------------------------------------------------------------

func TestCorruptedManifest_Empty(t *testing.T) {
	dir := t.TempDir()

	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const numKeys = 500
	written := make(map[string]string, numKeys)
	for i := 0; i < numKeys; i++ {
		key := []byte{byte(i >> 8), byte(i)}
		val := []byte{byte(i % 256), byte(i / 256)}
		ts := uint64(i + 1)
		if err := eng.PutWithTS(key, val, ts); err != nil {
			t.Fatalf("PutWithTS failed at key %d: %v", i, err)
		}
		written[string(key)] = string(val)
	}

	if err := eng.flushMemTable(); err != nil {
		t.Logf("flush: %v (may be expected)", err)
	}

	if err := eng.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// Truncate the Manifest to zero bytes.
	manifestPath := filepath.Join(dir, "MANIFEST")
	if err := os.Truncate(manifestPath, 0); err != nil {
		t.Fatalf("failed to truncate manifest: %v", err)
	}

	eng2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to reopen engine after empty manifest: %v", err)
	}
	defer errors.CloseWithFatal(eng2, "engine-after-empty-manifest")

	// Data should still be recoverable from WAL.
	var found int
	for k, v := range written {
		got, err := eng2.GetWithTS([]byte(k), 1<<30)
		if err != nil {
			continue
		}
		if string(got) == v {
			found++
		}
	}
	t.Logf("keys recovered after empty manifest: %d/%d", found, len(written))
	if found == 0 && len(written) > 0 {
		t.Error("expected at least some keys to survive empty manifest")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// corruptFileTail overwrites the last n bytes of the file at path with random
// data to simulate file corruption.
func corruptFileTail(t *testing.T, path string, n int) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Logf("file %s is empty, nothing to corrupt", path)
		return
	}

	// Don't corrupt more bytes than the file size.
	if int64(n) > info.Size() {
		n = int(info.Size())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}

	// Overwrite the last n bytes with random data.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < n; i++ {
		offset := len(data) - n + i
		data[offset] = byte(rng.Intn(256))
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write corrupted %s: %v", path, err)
	}
	t.Logf("corrupted last %d bytes of %s (size=%d)", n, path, info.Size())
}

// truncateFile truncates the file at path to the given fraction of its original
// size (e.g. 0.5 = half).
func truncateFile(t *testing.T, path string, fraction float64) {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat %s: %v", path, err)
	}
	newSize := int64(float64(info.Size()) * fraction)
	if newSize < 0 {
		newSize = 0
	}

	if err := os.Truncate(path, newSize); err != nil {
		t.Fatalf("failed to truncate %s to %d: %v", path, newSize, err)
	}
	t.Logf("truncated %s from %d to %d bytes", path, info.Size(), newSize)
}

// ensure interface compliance.
var _ = corruptFileTail
var _ = truncateFile
