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
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"
)

// retiredNode is a single entry in the epoch-based reclamation list.
// A pointer is safe to reclaim only when no reader holds an epoch at or
// below the recorded epoch.
type retiredNode struct {
	ptr   unsafe.Pointer
	epoch uint64
	next  *retiredNode
}

// EpochManager implements Epoch-Based Reclamation (EBR).
//
// Readers call EnterEpoch/ExitEpoch around each lock-free traversal.
// EnterEpoch atomically increments activeReaders. Writers retire nodes into an
// epoch-tagged reclamation list. Reset() waits for activeReaders to reach zero
// before mutating the arena; the atomic counter provides the happens-before
// ordering required for -race correctness: the arena zeroing is ordered after
// the last reader exit, so no reader observes partially-reset arena bytes.
type EpochManager struct {
	// activeReaders is incremented on EnterEpoch and decremented on ExitEpoch.
	// A zero value means no reader is inside a critical section.
	activeReaders int64

	// globalEpoch is incremented by AdvanceEpoch.
	globalEpoch uint64

	// retired is the pending reclamation list (mutex-protected).
	retired  *retiredNode
	tail     *retiredNode
	mu       sync.Mutex // protects retired list and globalEpoch
	retiredN int64      // atomic count of retired pointers (stats)
	statsA   int64      // atomic count of EnterEpoch (stats)
}

// NewEpochManager creates a new EpochManager.
func NewEpochManager(_ int) *EpochManager {
	return &EpochManager{}
}

// EnterEpoch begins a read-side critical section and returns the current epoch.
// Must be balanced by ExitEpoch.
func (em *EpochManager) EnterEpoch() uint64 {
	atomic.AddInt64(&em.activeReaders, 1)
	atomic.AddInt64(&em.statsA, 1)
	return atomic.LoadUint64(&em.globalEpoch)
}

// ExitEpoch ends a read-side critical section.
func (em *EpochManager) ExitEpoch() {
	atomic.AddInt64(&em.activeReaders, -1)
}

// ActiveReaders returns the current number of active readers.
func (em *EpochManager) ActiveReaders() int64 {
	return atomic.LoadInt64(&em.activeReaders)
}

// WaitForReaders spins until no reader is inside a critical section.
// After it returns, no reader holds a reference to any node, so the arena may
// be safely mutated. The reset path is cold, so a spin with runtime.Gosched
// is adequate.
func (em *EpochManager) WaitForReaders() {
	for atomic.LoadInt64(&em.activeReaders) != 0 {
		runtime.Gosched()
	}
}

// Quiesce waits until no reader is inside a critical section.
func (em *EpochManager) Quiesce() {
	em.WaitForReaders()
}

// ResumeQuiescence allows readers to enter again after a reset.
func (em *EpochManager) ResumeQuiescence() {
	// Readers may freely enter once the reset is complete.
}

// Retire records a node for deferred reclamation.
func (em *EpochManager) Retire(ptr unsafe.Pointer) {
	if ptr == nil {
		return
	}
	em.mu.Lock()
	epoch := atomic.LoadUint64(&em.globalEpoch)
	node := &retiredNode{ptr: ptr, epoch: epoch}
	if em.tail == nil {
		em.retired = node
		em.tail = node
	} else {
		em.tail.next = node
		em.tail = node
	}
	atomic.AddInt64(&em.retiredN, 1)
	em.mu.Unlock()
}

// Clean drains the pending reclamation list. The SkipList uses a single flat
// arena, so retired nodes are reclaimed by the arena reset. Clean exists for
// API compatibility and to keep the list bounded.
func (em *EpochManager) Clean() {
	em.mu.Lock()
	em.retired = nil
	em.tail = nil
	em.mu.Unlock()
}

// AdvanceEpoch increases the global epoch.
func (em *EpochManager) AdvanceEpoch() {
	em.mu.Lock()
	atomic.AddUint64(&em.globalEpoch, 1)
	em.mu.Unlock()
}

// Stats returns (activeReaders, retiredCount).
func (em *EpochManager) Stats() (int, int) {
	em.mu.Lock()
	count := 0
	for cur := em.retired; cur != nil; cur = cur.next {
		count++
	}
	em.mu.Unlock()
	return int(em.ActiveReaders()), count
}
