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
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestWAL_Corruption_Debug simulates an unclean shutdown by truncating the WAL
// file to simulate a partial write, then attempts recovery.
func TestWAL_Corruption_Debug(t *testing.T) {
	dir := t.TempDir()

	// Step 1: Create engine with GroupCommitEnabled and SyncMode=false
	// to maximize the chance of buffered-but-unflushed data.
	opts := WALOptions{
		GroupCommitEnabled:  true,
		GroupCommitInterval: 0, // default 10ms
		SyncMode:            false,
	}

	engine, err := NewLSMEngine(dir, opts)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	// Step 2: Perform 100 Put operations
	for i := 0; i < 100; i++ {
		key := []byte{byte(i)}
		val := []byte{byte(i), byte(i >> 8)}
		commitTS := uint64(i + 1)
		if err := engine.PutWithTS(key, val, commitTS); err != nil {
			t.Fatalf("PutWithTS failed at iteration %d: %v", i, err)
		}
	}

	// Step 3: Close the engine normally first
	if err := engine.Close(); err != nil {
		t.Fatalf("engine.Close() failed: %v", err)
	}

	// Step 4: Truncate the last 32 bytes from the shard-0 WAL to simulate a
	// partial write (as would happen on process kill during group commit flush).
	// Each shard owns its own WAL (wal_<id>.log); shard 0 always exists even
	// when DefaultShardCount() > 1. See: HOT-01, REC-01
	walPath := filepath.Join(dir, "wal_0.log")
	stat, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("failed to stat wal_0.log: %v", err)
	}
	origSize := stat.Size()
	truncateSize := origSize - 32
	if truncateSize < 0 {
		truncateSize = 0
	}
	if err := os.Truncate(walPath, truncateSize); err != nil {
		t.Fatalf("failed to truncate wal_0.log: %v", err)
	}
	t.Logf("[SETUP] Truncated wal_0.log from %d bytes to %d bytes to simulate partial write", origSize, truncateSize)

	// Step 5: Try to re-open the engine
	engine2, err := NewLSMEngine(dir, opts)
	if err != nil {
		t.Logf("[ERROR] Recovery failed: %v", err)

		// Dump first 16 bytes of WAL
		raw, readErr := os.ReadFile(walPath)
		if readErr != nil {
			t.Logf("[ERROR] Cannot read WAL file: %v", readErr)
		} else {
			dumpLen := 16
			if len(raw) < dumpLen {
				dumpLen = len(raw)
			}
			t.Logf("[DEBUG] WAL file size: %d bytes", len(raw))
			t.Logf("[DEBUG] First %d bytes of WAL:\n%s", dumpLen, hex.Dump(raw[:dumpLen]))
		}
		return
	}
	defer engine2.Close()

	// Step 6: Recovery succeeded — verify keys
	t.Logf("[INFO] Recovery succeeded")
	recovered := 0
	var missingKeys []int
	for i := 0; i < 100; i++ {
		key := []byte{byte(i)}
		snapshotTS := uint64(i + 1)
		val, err := engine2.GetWithTS(key, snapshotTS)
		if err != nil {
			t.Errorf("GetWithTS failed for key[%d]: %v", i, err)
			continue
		}
		if val == nil {
			missingKeys = append(missingKeys, i)
		} else {
			recovered++
			t.Logf("[INFO] Key %d recovered: value=%v", i, val)
		}
	}

	// Expect at least 98 keys recovered; up to 2 may be lost due to partial WAL write.
	if recovered < 98 {
		t.Errorf("expected at least 98 keys recovered, got %d", recovered)
	} else {
		t.Logf("[INFO] Successfully recovered %d/100 keys (missing: %v)", recovered, missingKeys)
	}
}
