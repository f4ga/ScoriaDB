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
	"sync/atomic"
)

// RegisterSnapshot registers an active snapshot with the given timestamp.
func (e *LSMEngine) RegisterSnapshot(snapshotTS uint64) {
	for {
		oldMin := atomic.LoadUint64(&e.minActiveSnapshotTS)
		if oldMin == 0 || snapshotTS < oldMin {
			if atomic.CompareAndSwapUint64(&e.minActiveSnapshotTS, oldMin, snapshotTS) {
				return
			}
			continue
		}
		return
	}
}

// UnregisterSnapshot unregisters an active snapshot.
func (e *LSMEngine) UnregisterSnapshot(snapshotTS uint64) {
	oldMin := atomic.LoadUint64(&e.minActiveSnapshotTS)
	if oldMin == snapshotTS {
		atomic.StoreUint64(&e.minActiveSnapshotTS, 0)
	}
}

// GetMinActiveSnapshotTS returns the minimum timestamp among active snapshots.
func (e *LSMEngine) GetMinActiveSnapshotTS() uint64 {
	return atomic.LoadUint64(&e.minActiveSnapshotTS)
}
