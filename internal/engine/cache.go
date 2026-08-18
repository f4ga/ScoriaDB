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

// maxLastCommitCacheEntries limits the number of keys tracked in the
// lastCommitCache. Without a limit, a workload with many unique keys
// would grow the map without bound and eventually cause OOM.
//
// 10_000 is enough to cover hot working sets while keeping memory bounded.
// This is a defensive limit, not a performance tuning knob.
const maxLastCommitCacheEntries = 10_000

// lastCommitCacheEntry is stored in the LRU list and maps back to the key
// so we can delete it from the map when it is evicted.
type lastCommitCacheEntry struct {
	key string
	ts  uint64
}

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
//
// The cache is bounded by maxLastCommitCacheEntries. When the cache grows
// past that limit, the least recently used entries are evicted. This trades
// a tiny amount of cache-miss overhead for a hard guarantee that the map
// cannot grow without bound.
func (e *LSMEngine) updateLastCommitCache(key []byte, commitTS uint64) {
	stableKey := lastCommitCacheKey(key)

	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()

	// If the key already exists, move it to the front of the LRU list.
	if elem, ok := e.lastCommitCacheMap[stableKey]; ok {
		elem.Value.(*lastCommitCacheEntry).ts = commitTS
		e.lastCommitCacheLRU.MoveToFront(elem)
		return
	}

	// New key: insert into map and front of LRU list.
	entry := &lastCommitCacheEntry{key: stableKey, ts: commitTS}
	elem := e.lastCommitCacheLRU.PushFront(entry)
	e.lastCommitCacheMap[stableKey] = elem

	// Evict oldest entries while the cache is over the limit.
	for e.lastCommitCacheLRU.Len() > maxLastCommitCacheEntries {
		oldest := e.lastCommitCacheLRU.Back()
		if oldest == nil {
			break
		}
		evict := oldest.Value.(*lastCommitCacheEntry)
		delete(e.lastCommitCacheMap, evict.key)
		e.lastCommitCacheLRU.Remove(oldest)
	}
}

// getLastCommitCache returns the last commit timestamp for a key from cache.
// Uses lastCommitCacheKey so lookups match keys written with a stable copy.
//
// Successful lookups are promoted to the front of the LRU list, so the cache
// behaves as a true LRU: hot keys stay, cold keys get evicted.
func (e *LSMEngine) getLastCommitCache(key []byte) (uint64, bool) {
	stableKey := lastCommitCacheKey(key)

	e.cacheMu.RLock()
	elem, ok := e.lastCommitCacheMap[stableKey]
	e.cacheMu.RUnlock()
	if !ok {
		return 0, false
	}

	e.cacheMu.Lock()
	if elem, stillExists := e.lastCommitCacheMap[stableKey]; stillExists {
		e.lastCommitCacheLRU.MoveToFront(elem)
	}
	ts := elem.Value.(*lastCommitCacheEntry).ts
	e.cacheMu.Unlock()

	return ts, true
}
