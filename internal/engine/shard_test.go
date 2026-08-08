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
	"bytes"
	"sync"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

// TestDefaultShardCount verifies that the engine creates at least one shard and
// never exceeds the configured cap. See: HOT-01
func TestDefaultShardCount(t *testing.T) {
	n := DefaultShardCount()
	if n < 1 {
		t.Fatalf("DefaultShardCount returned %d, want >= 1", n)
	}
	if n > 16 {
		t.Fatalf("DefaultShardCount returned %d, want <= 16", n)
	}
}

// TestShardIndexDeterministic verifies that the same key always maps to the
// same shard, and that a set of distinct keys distributes across shards.
// See: HOT-01
func TestShardIndexDeterministic(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	if len(eng.shards) == 0 {
		t.Fatal("engine has no shards")
	}

	// Determinism: same key → same shard.
	key := []byte("deterministic-key")
	if a, b := eng.shardIndex(key), eng.shardIndex(key); a != b {
		t.Fatalf("shard index not deterministic: %d != %d", a, b)
	}

	// Distribution: many distinct keys should land in more than one shard
	// (on a multi-core machine). This validates that hash routing actually
	// splits the write path rather than collapsing to a single shard.
	seen := make(map[int]struct{})
	for i := 0; i < 1024; i++ {
		k := []byte{byte(i), byte(i >> 8), byte(i >> 16), byte(i >> 24)}
		seen[eng.shardIndex(k)] = struct{}{}
	}
	if len(eng.shards) > 1 && len(seen) < 2 {
		t.Fatalf("hash routing collapsed %d keys into 1 shard", 1024)
	}
}

// TestShardedWritesDistributeAcrossShards verifies that keys hash to different
// shards and that each shard's memTable holds the keys routed to it.
// See: HOT-01
func TestShardedWritesDistributeAcrossShards(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	if len(eng.shards) < 2 {
		t.Skip("requires at least 2 shards to verify distribution")
	}

	// Write enough distinct keys to guarantee at least two shards are hit.
	wrote := 0
	for i := 0; i < 512 && wrote < 64; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x42, 0x00}
		if err := eng.PutWithTS(key, []byte("v"), uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
		wrote++
	}

	// Verify reads find data regardless of which shard it was routed to.
	eng.mu.RLock()
	nonEmptyShards := 0
	for _, shard := range eng.shards {
		if shard.memTable != nil && shard.memTable.Size() > 0 {
			nonEmptyShards++
		}
	}
	eng.mu.RUnlock()

	if nonEmptyShards < 2 {
		t.Fatalf("expected writes to spread across shards, but only %d shard(s) are non-empty", nonEmptyShards)
	}

	// Every written key must be readable via the engine-level API.
	for i := 0; i < 64; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x42, 0x00}
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS failed for key %d: %v", i, err)
		}
		if !bytes.Equal(got, []byte("v")) {
			t.Errorf("key %d: expected 'v', got %q", i, got)
		}
	}
}

// TestShardedReadsAcrossAllShards verifies that GetWithTS, GetLatestInfo, and
// Scan return consistent results across keys in every shard.
// See: HOT-01
func TestShardedReadsAcrossAllShards(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	const n = 256
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0xAA, 0xBB}
		val := []byte{byte(i)}
		if err := eng.PutWithTS(key, val, uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	// GetWithTS across all keys.
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0xAA, 0xBB}
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS failed: %v", err)
		}
		if !bytes.Equal(got, []byte{byte(i)}) {
			t.Errorf("GetWithTS key %d: got %v", i, got)
		}
	}

	// GetLatestInfo across all keys.
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0xAA, 0xBB}
		val, ts, found, err := eng.GetLatestInfo(key)
		if err != nil {
			t.Fatalf("GetLatestInfo failed: %v", err)
		}
		if !found || !bytes.Equal(val, []byte{byte(i)}) || ts != uint64(i+1) {
			t.Errorf("GetLatestInfo key %d: val=%v ts=%d found=%v", i, val, ts, found)
		}
	}

	// Scan across all shards.
	iter := eng.Scan(nil)
	count := 0
	for iter.Next() {
		count++
	}
	iter.Close()
	if count != n {
		t.Errorf("Scan returned %d keys, want %d", count, n)
	}
}

// TestShardedConcurrentWrites runs concurrent writes with distinct keys and
// verifies they are all readable afterwards. Run with -race to detect data races
// in the sharded write path.
// See: HOT-01, RACE-01
func TestShardedConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	const workers = 8
	const perWorker = 128

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := []byte{byte(id), byte(i), byte(i >> 8), 0x7F}
				if err := eng.PutWithTS(key, []byte{byte(id)}, uint64(id*perWorker+i+1)); err != nil {
					t.Errorf("PutWithTS failed: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	// Verify every concurrent write is visible.
	for w := 0; w < workers; w++ {
		for i := 0; i < perWorker; i++ {
			key := []byte{byte(w), byte(i), byte(i >> 8), 0x7F}
			got, err := eng.GetWithTS(key, uint64(w*perWorker+i+1))
			if err != nil {
				t.Fatalf("GetWithTS failed: %v", err)
			}
			if !bytes.Equal(got, []byte{byte(w)}) {
				t.Errorf("worker %d key %d: got %v", w, i, got)
			}
		}
	}
}

// TestShardedFlushDrainsAllShards verifies that flush drains every shard's
// MemTable into SSTables and data remains readable afterwards.
// See: HOT-01, FLUSH-01
func TestShardedFlushDrainsAllShards(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	if len(eng.shards) < 2 {
		t.Skip("requires at least 2 shards")
	}

	const n = 128
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		if err := eng.PutWithTS(key, []byte{byte(i)}, uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS failed: %v", err)
		}
	}

	// Force a flush. It must drain every non-empty shard into an SSTable.
	if err := eng.flushMemTable(); err != nil {
		t.Fatalf("flushMemTable failed: %v", err)
	}

	// All shard MemTables should now be empty (drained to SSTable).
	eng.mu.RLock()
	for idx, shard := range eng.shards {
		if shard.memTable.Size() != 0 {
			eng.mu.RUnlock()
			t.Fatalf("shard %d still has %d entries after flush", idx, shard.memTable.Size())
		}
	}
	level0Count := len(eng.levels[0])
	eng.mu.RUnlock()

	if level0Count == 0 {
		t.Fatal("expected at least one Level0 SSTable after flush")
	}

	// Data must still be readable from the flushed SSTables.
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS after flush failed: %v", err)
		}
		if !bytes.Equal(got, []byte{byte(i)}) {
			t.Errorf("key %d after flush: got %v", i, got)
		}
	}
}
