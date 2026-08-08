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

import "sync"

// snapshotRegistry tracks active MVCC snapshots with reference counting.
//
// Multiple snapshots may be active concurrently with different (or even equal)
// timestamps. The minimum active snapshot TS is the watermark below which
// compaction may safely discard versions. A naive implementation that simply
// stores the min would incorrectly reset the watermark to 0 when a non-minimum
// snapshot closes while others remain active. Reference counting per TS solves
// this: UnregisterSnapshot only lowers the watermark to the next minimum among
// still-active snapshots (or 0 when none remain).
//
// The structure is guarded by a single RWMutex. Hot path (RegisterSnapshot) takes
// a write lock, but snapshot registration/deregistration is a cold path relative
// to key reads/writes, so this is acceptable.
type snapshotRegistry struct {
	mu     sync.RWMutex
	counts map[uint64]int // number of active snapshots per TS
	min    uint64         // current minimum active snapshot TS (0 == none)
}

// newSnapshotRegistry returns an empty snapshot registry.
func newSnapshotRegistry() *snapshotRegistry {
	return &snapshotRegistry{
		counts: make(map[uint64]int),
	}
}

// RegisterSnapshot records a new active snapshot with the given TS.
//
// Reference counting allows multiple transactions sharing the same startTS to
// register independently; the snapshot stays active until every holder
// unregisters. The watermark is lowered if the new TS is smaller than the
// current minimum (or if no snapshot is active yet).
func (r *snapshotRegistry) RegisterSnapshot(snapshotTS uint64) {
	r.mu.Lock()
	r.counts[snapshotTS]++
	if r.min == 0 || snapshotTS < r.min {
		r.min = snapshotTS
	}
	r.mu.Unlock()
}

// UnregisterSnapshot removes one active snapshot reference for the given TS.
//
// The watermark is recomputed from the remaining counts:
//   - If no snapshots remain at all, the watermark resets to 0.
//   - Otherwise it is set to the smallest still-active TS.
//
// Unregistering a TS with no active reference is a no-op (defensive against
// double-close bugs) and never panics.
func (r *snapshotRegistry) UnregisterSnapshot(snapshotTS uint64) {
	r.mu.Lock()
	count, ok := r.counts[snapshotTS]
	if !ok {
		// No active snapshot for this TS. Defensive no-op: this can happen if a
		// caller double-closes a transaction or closes one that never registered.
		r.mu.Unlock()
		return
	}
	if count > 1 {
		r.counts[snapshotTS] = count - 1
	} else {
		delete(r.counts, snapshotTS)
	}
	r.recomputeMinLocked()
	r.mu.Unlock()
}

// Min returns the current minimum active snapshot TS, or 0 if none are active.
func (r *snapshotRegistry) Min() uint64 {
	r.mu.RLock()
	min := r.min
	r.mu.RUnlock()
	return min
}

// recomputeMinLocked recalculates r.min from r.counts. Caller must hold r.mu.
func (r *snapshotRegistry) recomputeMinLocked() {
	if len(r.counts) == 0 {
		r.min = 0
		return
	}
	var newMin uint64
	first := true
	for ts := range r.counts {
		if first || ts < newMin {
			newMin = ts
			first = false
		}
	}
	r.min = newMin
}

// RegisterSnapshot registers an active snapshot with the given timestamp.
// See snapshotRegistry.RegisterSnapshot.
func (e *LSMEngine) RegisterSnapshot(snapshotTS uint64) {
	e.snapshotRegistry.RegisterSnapshot(snapshotTS)
}

// UnregisterSnapshot unregisters an active snapshot.
// See snapshotRegistry.UnregisterSnapshot.
func (e *LSMEngine) UnregisterSnapshot(snapshotTS uint64) {
	e.snapshotRegistry.UnregisterSnapshot(snapshotTS)
}

// GetMinActiveSnapshotTS returns the minimum timestamp among active snapshots,
// or 0 if no snapshots are active. Compaction uses this as the safety watermark
// below which MVCC versions may be discarded.
func (e *LSMEngine) GetMinActiveSnapshotTS() uint64 {
	return e.snapshotRegistry.Min()
}
