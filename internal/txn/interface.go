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

package txn

// BatchWriter defines the interface for atomic batch writes.
type BatchWriter interface {
	WriteAtomicBatch(data []byte, commitTS uint64) error
}

// TimestampProvider defines the interface for timestamp generation.
type TimestampProvider interface {
	NextTimestamp() uint64
}

// SnapshotManager defines the interface for snapshot registration.
type SnapshotManager interface {
	RegisterSnapshot(snapshotTS uint64)
	UnregisterSnapshot(snapshotTS uint64)
}

// ConflictChecker defines the interface for conflict detection.
type ConflictChecker interface {
	CheckConflict(key []byte, startTS uint64) (bool, error)
}

// KVReader defines the interface for key-value reads with snapshot isolation.
type KVReader interface {
	GetWithTS(key []byte, snapshotTS uint64) ([]byte, error)
}

// Engine is the combined interface needed by transactions.
type Engine interface {
	BatchWriter
	TimestampProvider
	SnapshotManager
	ConflictChecker
	KVReader
}
