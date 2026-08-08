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

// Package engine implements the core LSM-tree storage engine for ScoriaDB,
// including MemTable, SSTable, WAL, Value Log, compaction, snapshots,
// and background flush/compaction workers.
package engine

// lastCommitCacheKey returns a stable copy of key suitable for use as a map key.
//
// The key is explicitly copied so the returned string does NOT share memory
// with the input []byte. If the caller mutates the byte slice after the cache
// write, the map key must remain unchanged, otherwise CheckConflict could
// return wrong results and break Snapshot Isolation.
//
// This is the COLD path (cache read/write, protected by cacheMu) so the
// allocation is acceptable. See: ARCH-07.
func lastCommitCacheKey(key []byte) string {
	return string(append([]byte(nil), key...))
}

// updateLastCommitCache updates the last commit timestamp cache for a key.
// Uses lastCommitCacheKey to store a stable (copied) string key, so the cache
// entry is immune to later mutation of the caller's key slice.
func (e *LSMEngine) updateLastCommitCache(key []byte, commitTS uint64) {
	e.cacheMu.Lock()
	e.lastCommitCache[lastCommitCacheKey(key)] = commitTS
	e.cacheMu.Unlock()
}

// getLastCommitCache returns the last commit timestamp for a key from cache.
// Uses lastCommitCacheKey so lookups match keys written with a stable copy.
func (e *LSMEngine) getLastCommitCache(key []byte) (uint64, bool) {
	e.cacheMu.RLock()
	defer e.cacheMu.RUnlock()
	ts, ok := e.lastCommitCache[lastCommitCacheKey(key)]
	return ts, ok
}
