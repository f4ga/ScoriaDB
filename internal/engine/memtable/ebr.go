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

package memtable

import (
	"sync/atomic"
	"unsafe"
)

// ============================================================
// EpochManager — Minimal EBR for Future Use
// ============================================================
//
// In the current architecture, EBR is NOT needed for correctness:
//   - Arena is grow-only: nodes are never freed until Reset()
//   - Reset() is called when no goroutines are accessing the skip list
//   - Delete() only sets a tombstone flag, the node remains in memory
//
// EBR is kept as infrastructure for future use (e.g., node-level compaction).
// EnterEpoch/ExitEpoch are no-ops — zero cost in the hot path.
// Retire is also a no-op since arena memory is never reclaimed mid-session.
//
// See: PROMPT-EBR-FIX-V2, ARCH-11, BL-02, GL-08

// EpochManager manages epoch-based reclamation.
// Currently a no-op placeholder — EnterEpoch/ExitEpoch cost nothing.
// Fields mu, retired, counter, maxEpochs removed as dead code;
// they will be re-added when EBR is fully implemented.
type EpochManager struct {
	global    atomic.Int64
	threshold int64
}

// NewEpochManager creates a new EpochManager.
func NewEpochManager(threshold int) *EpochManager {
	return &EpochManager{
		threshold: int64(threshold),
	}
}

// EnterEpoch is a no-op in the current architecture.
// Arena is grow-only, so no memory reclamation happens during reads.
// Zero cost in hot path.
//
//go:nosplit
func (em *EpochManager) EnterEpoch() int64 {
	return em.global.Load()
}

// ExitEpoch is a no-op in the current architecture.
// Zero cost in hot path.
//
//go:nosplit
func (em *EpochManager) ExitEpoch() {
	// no-op
}

// Retire is a no-op in the current architecture.
// Arena memory is never reclaimed until Reset().
func (em *EpochManager) Retire(ptr unsafe.Pointer) {
	// no-op: arena is grow-only, nodes are never freed mid-session
}

// Clean is a no-op in the current architecture.
func (em *EpochManager) Clean() {
	// no-op
}

// AdvanceEpoch advances the global epoch.
func (em *EpochManager) AdvanceEpoch() {
	em.global.Add(1)
}

// Stats returns the number of active slots and retired nodes (for monitoring).
func (em *EpochManager) Stats() (active int, retired int) {
	return 0, 0
}
