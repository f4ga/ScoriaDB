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

// EpochManager manages epoch-based reclamation of nodes.
type EpochManager struct {
	global  atomic.Int64
	threads sync.Map // goroutineID → epoch
	retired [maxEpochs][]unsafe.Pointer
	mu      sync.Mutex
	counter int64
}

var epochManager = &EpochManager{}

// getGoroutineID returns a unique identifier for the current goroutine.
func getGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	var id uint64
	for i := 0; i < n; i++ {
		if buf[i] == ' ' {
			for j := i + 1; j < n; j++ {
				if buf[j] >= '0' && buf[j] <= '9' {
					id = id*10 + uint64(buf[j]-'0')
				} else {
					return id
				}
			}
			return id
		}
	}
	return 0
}

// EnterEpoch registers the current goroutine in the current global epoch.
// Returns the epoch number.
func (em *EpochManager) EnterEpoch() int64 {
	id := getGoroutineID()
	epoch := em.global.Load()
	em.threads.Store(id, epoch)
	return epoch
}

// ExitEpoch removes the current goroutine from the epoch mapping.
func (em *EpochManager) ExitEpoch() {
	id := getGoroutineID()
	em.threads.Delete(id)
}

// Retire adds a node pointer to the retirement list for the current epoch.
// The node will be freed once no thread is in its epoch.
func (em *EpochManager) Retire(ptr unsafe.Pointer) {
	em.mu.Lock()
	defer em.mu.Unlock()
	epoch := em.global.Load() % maxEpochs
	em.retired[epoch] = append(em.retired[epoch], ptr)

	// Trigger cleanup every 1000 retirements
	em.counter++
	if em.counter%1000 == 0 {
		em.cleanLocked()
	}
}

// cleanLocked frees all nodes whose epoch is no longer active.
// Must be called with em.mu held.
func (em *EpochManager) cleanLocked() {
	// Collect active epochs
	activeEpochs := make(map[int64]struct{})
	em.threads.Range(func(_, value any) bool {
		if epoch, ok := value.(int64); ok {
			activeEpochs[epoch] = struct{}{}
		}
		return true
	})

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
	em.mu.Lock()
	defer em.mu.Unlock()
	em.cleanLocked()
}

// AdvanceEpoch advances the global epoch and triggers cleanup.
func (em *EpochManager) AdvanceEpoch() {
	em.global.Add(1)
	em.Clean()
}
