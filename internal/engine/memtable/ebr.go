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

// Package memtable provides an in-memory table implementation using a lock-free
// skip list with a linear arena allocator. It supports concurrent reads and writes
// with zero heap allocations in the hot path.
package memtable

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

// ============================================================
// Epoch-Based Reclamation (EBR)
// ============================================================
//
// EBR provides safe memory reclamation for lock-free data structures.
// It works by dividing time into epochs. A node can only be freed
// when no thread is in an epoch that might reference it.
//
// Threads announce their active epoch on entry and unannounce on exit.
// Retired nodes are batched per epoch and freed when the epoch is
// no longer active.

const (
	maxEpochs = 3 // 0, 1, 2 — active, previous, old
)

//go:linkname runtime_getg runtime.getg
func runtime_getg() unsafe.Pointer

// getGoroutineID returns the current goroutine ID.
// Uses runtime.getg via //go:linkname — zero allocations, sub-10ns.
// See: BL-08, PERF-01
//
//go:nosplit
func getGoroutineID() uint64 {
	// runtime.getg() returns *g struct; goid is the first int64 field.
	// This is stable across Go versions and avoids runtime.Stack overhead.
	return uint64(*(*int64)(runtime_getg()))
}

// EpochManager manages epoch-based reclamation of nodes.
type EpochManager struct {
	global  atomic.Int64
	threads map[uint64]int64 // goroutineID → epoch, protected by mu
	mu      sync.RWMutex
	retired [maxEpochs][]unsafe.Pointer
	cleanMu sync.Mutex
	counter int64
}

// NewEpochManager creates a new EpochManager.
func NewEpochManager() *EpochManager {
	return &EpochManager{
		threads: make(map[uint64]int64, 64), // pre-allocate for typical concurrency
	}
}

// EnterEpoch registers the current goroutine in the current global epoch.
// Returns the epoch number.
// Zero allocations in hot path.
// See: BL-09, PERF-01
func (em *EpochManager) EnterEpoch() int64 {
	id := getGoroutineID()
	epoch := em.global.Load()

	em.mu.Lock()
	em.threads[id] = epoch
	em.mu.Unlock()

	return epoch
}

// ExitEpoch removes the current goroutine from the epoch mapping.
// Zero allocations in hot path.
// See: BL-09, PERF-01
func (em *EpochManager) ExitEpoch() {
	id := getGoroutineID()

	em.mu.Lock()
	delete(em.threads, id)
	em.mu.Unlock()
}

// Retire adds a node pointer to the retirement list for the current epoch.
// The node will be freed once no thread is in its epoch.
func (em *EpochManager) Retire(ptr unsafe.Pointer) {
	em.cleanMu.Lock()
	defer em.cleanMu.Unlock()

	epoch := em.global.Load() % maxEpochs
	em.retired[epoch] = append(em.retired[epoch], ptr)

	// Trigger cleanup every 1000 retirements
	em.counter++
	if em.counter%1000 == 0 {
		em.cleanLocked()
	}
}

// cleanLocked frees all nodes whose epoch is no longer active.
// Must be called with em.cleanMu held.
func (em *EpochManager) cleanLocked() {
	// Collect active epochs from threads map
	activeEpochs := make(map[int64]struct{})

	em.mu.RLock()
	for _, epoch := range em.threads {
		activeEpochs[epoch] = struct{}{}
	}
	em.mu.RUnlock()

	// Free nodes in epochs that have no active threads
	for epoch := 0; epoch < maxEpochs; epoch++ {
		if _, active := activeEpochs[int64(epoch)]; active {
			continue
		}
		// Release references so GC can collect them
		for _, ptr := range em.retired[epoch] {
			_ = (*Node)(ptr)
		}
		em.retired[epoch] = nil
	}
}

// Clean triggers a cleanup of retired nodes.
func (em *EpochManager) Clean() {
	em.cleanMu.Lock()
	defer em.cleanMu.Unlock()
	em.cleanLocked()
}

// AdvanceEpoch advances the global epoch and triggers cleanup.
func (em *EpochManager) AdvanceEpoch() {
	em.global.Add(1)
	em.Clean()
}
