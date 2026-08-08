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
	"sync"
	"testing"
	"unsafe"
)

// ============================================================
// EpochManager tests
// ============================================================
//
// EBR is currently a no-op placeholder. Tests verify that
// all methods can be called without panics or races.

func TestEpochManagerNew(t *testing.T) {
	em := NewEpochManager(1000)
	if em == nil {
		t.Fatal("NewEpochManager returned nil")
	}
}

func TestEpochManagerEnterExitEpoch(t *testing.T) {
	em := NewEpochManager(1000)

	em.EnterEpoch()
	// EnterEpoch returns the current global epoch value

	// ExitEpoch should not panic
	em.ExitEpoch()
}

func TestEpochManagerExitWithoutEnter(t *testing.T) {
	em := NewEpochManager(1000)
	// ExitEpoch without EnterEpoch should not panic
	em.ExitEpoch()
}

func TestEpochManagerRetire(t *testing.T) {
	em := NewEpochManager(1000)

	var dummy int
	ptr := unsafe.Pointer(&dummy)

	// Retire should not panic
	em.Retire(ptr)
}

func TestEpochManagerRetireNil(t *testing.T) {
	em := NewEpochManager(1000)
	// Retire with nil pointer should not panic
	em.Retire(nil)
}

func TestEpochManagerClean(t *testing.T) {
	em := NewEpochManager(1000)

	// Clean on empty manager should not panic
	em.Clean()

	// Clean after Retire should not panic
	var dummy int
	em.Retire(unsafe.Pointer(&dummy))
	em.Clean()
}

func TestEpochManagerAdvanceEpoch(t *testing.T) {
	em := NewEpochManager(1000)

	// AdvanceEpoch should not panic
	em.AdvanceEpoch()
	em.AdvanceEpoch()
	em.AdvanceEpoch()
}

func TestEpochManagerStats(t *testing.T) {
	em := NewEpochManager(1000)

	active, retired := em.Stats()
	if active != 0 {
		t.Errorf("expected active=0, got %d", active)
	}
	if retired != 0 {
		t.Errorf("expected retired=0, got %d", retired)
	}
}

func TestEpochManagerConcurrentEnterExit(t *testing.T) {
	em := NewEpochManager(1000)
	var wg sync.WaitGroup
	numGoroutines := 20
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				em.EnterEpoch()
				em.ExitEpoch()
			}
		}()
	}
	wg.Wait()
}

func TestEpochManagerConcurrentRetire(t *testing.T) {
	em := NewEpochManager(1000)
	var wg sync.WaitGroup
	numGoroutines := 10
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var dummy int
			ptr := unsafe.Pointer(&dummy)
			for j := 0; j < iterations; j++ {
				em.Retire(ptr)
			}
		}()
	}
	wg.Wait()
}

func TestEpochManagerFullCycle(t *testing.T) {
	em := NewEpochManager(1000)

	// Simulate a full EBR cycle: enter, retire, advance, clean
	var dummy int
	ptr := unsafe.Pointer(&dummy)

	em.EnterEpoch()
	em.Retire(ptr)
	em.AdvanceEpoch()
	em.Clean()
	em.ExitEpoch()
}

func TestEpochManagerMultipleAdvance(t *testing.T) {
	em := NewEpochManager(1000)

	// Advance multiple times and verify no issues
	for i := 0; i < 100; i++ {
		em.AdvanceEpoch()
	}
}

func TestEpochManagerConcurrentMixed(t *testing.T) {
	em := NewEpochManager(1000)
	var wg sync.WaitGroup

	// Mix of EnterEpoch, ExitEpoch, Retire, AdvanceEpoch, Clean
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var dummy int
			ptr := unsafe.Pointer(&dummy)
			for j := 0; j < 50; j++ {
				em.EnterEpoch()
				em.Retire(ptr)
				em.AdvanceEpoch()
				em.Clean()
				em.ExitEpoch()
			}
		}()
	}
	wg.Wait()
}
