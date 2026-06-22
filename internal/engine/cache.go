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

// updateLastCommitCache updates the last commit timestamp cache for a key.
func (e *LSMEngine) updateLastCommitCache(key []byte, commitTS uint64) {
	e.cacheMu.Lock()
	e.lastCommitCache[string(key)] = commitTS
	e.cacheMu.Unlock()
}

// getLastCommitCache returns the last commit timestamp for a key from cache.
func (e *LSMEngine) getLastCommitCache(key []byte) (uint64, bool) {
	e.cacheMu.RLock()
	defer e.cacheMu.RUnlock()
	ts, ok := e.lastCommitCache[string(key)]
	return ts, ok
}
