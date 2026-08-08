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
	"runtime"
	"sync/atomic"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
)

// Shard is an independent write partition of the LSM engine.
//
// Each shard owns its own active MemTable (and therefore its own SkipList mutex)
// AND its own WAL (with its own mutex and group-commit writer). Concurrent writes
// with different keys hash to different shards and thus hit different SkipList
// mutexes AND different WAL mutexes. This eliminates BOTH global serialization
// points: the SkipList mutex and the WAL mutex.
//
// Without sharding the WAL, all writes still serialize on the single WAL.mu,
// which dominates the small-value write path and prevents scaling with cores.
// Each shard therefore gets an independent WAL file (wal_<id>.log).
//
// All shards share the engine-level Manifest and SSTable level set, keeping
// compaction semantics unchanged. Reads scan every shard's MemTables plus the
// shared SSTables.
//
// See: HOT-01, ARCH-10
type Shard struct {
	// id is the shard index (0..N-1). Used to name the shard's WAL file.
	id int

	// wal is this shard's own Write-Ahead Log. Serialization is bounded to this
	// shard only; different shards write to different WAL files concurrently.
	// See: HOT-01
	wal *WAL

	// memSize is the current total size (in bytes) of the active memTable.
	// Atomic — written via atomic.AddInt64 in the hot path, read by the
	// flush worker to decide when to flush. See: PERF-01
	memSize int64

	// memTable is the active (writable) memTable for this shard.
	// Protected by engine.mu against swap with frozenMemTable during flush.
	memTable *memtable.MemTable

	// frozenMemTable is the memTable currently being flushed to an SSTable.
	// Protected by engine.mu.
	frozenMemTable *memtable.MemTable
}

// NewShard creates a shard with a fresh active MemTable and the given ID.
// The WAL is wired up by the engine (NewLSMEngine) after opening the file.
func NewShard(id int) *Shard {
	return &Shard{
		id:       id,
		memTable: memtable.NewMemTable(),
	}
}

// DefaultShardCount returns the number of shards to create based on the
// available CPU count. A single shard (GOMAXPROCS=1) degrades gracefully to
// the legacy single-MemTable behavior. The count is bounded to avoid excessive
// memory overhead for write-heavy workloads.
func DefaultShardCount() int {
	n := runtime.GOMAXPROCS(0)
	if n < 1 {
		return 1
	}
	// Each shard pre-allocates an arena block (ArenaBlockSize = 64 MB
	// is the block size, but a fresh memTable reserves only a small header).
	// Cap the shard count to bound per-shard overhead while still enabling
	// near-linear scaling on typical multi-core machines.
	if n > 16 {
		return 16
	}
	return n
}

// hashKey returns a 64-bit FNV-1a hash of the key.
// FNV-1a is chosen over a cryptographic hash for the hot path: it is fast,
// allocation-free, and produces a good enough distribution for shard routing.
// Zero allocations, no syscalls.
//
//go:nosplit
//go:inline
func hashKey(key []byte) uint64 {
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)
	h := offset64
	for _, b := range key {
		h ^= uint64(b)
		h *= prime64
	}
	return h
}

// shardIndex returns the shard index for the given key.
// Zero allocations in the hot path.
//
//go:nosplit
//go:inline
func (e *LSMEngine) shardIndex(key []byte) int {
	// len(e.shards) is always a power of two within [1, 16], so the
	// compiler can turn this into a mask if len is a constant. We keep
	// the modulo for generality; it is a single ALU instruction.
	return int(hashKey(key) % uint64(len(e.shards)))
}

// shard returns the shard responsible for the given key.
//
//go:inline
func (e *LSMEngine) shard(key []byte) *Shard {
	return e.shards[e.shardIndex(key)]
}

// memSize returns the shard's current memTable size.
func (s *Shard) memSizeLoad() int64 { return atomic.LoadInt64(&s.memSize) }
