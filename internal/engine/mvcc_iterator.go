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
	"bytes"
)

// MVCCIterator wraps any Iterator and applies MVCC filtering:
//   - Returns only the latest non-deleted version per user key
//   - Tombstones are filtered out (if a key has been deleted, it is not returned)
//   - Zero heap allocations in the hot path
//
// The inner iterator must produce entries sorted by (userKey, Timestamp)
// where for the same user key, entries are ordered from oldest to newest
// (ascending commitTS). MVCCIterator collects all versions of the same
// user key and returns only the newest non-deleted version.
//
// See: ARCH-07, MVCC-03
type MVCCIterator struct {
	inner Iterator
	key   []byte
	value []byte
	err   error
	ended bool

	// peekedKey is set when we've consumed the next key from inner
	// while scanning versions of the current key. On the next call to
	// Next(), we use this instead of calling inner.Next() again.
	peeked bool
}

// NewMVCCIterator creates a new MVCCIterator wrapping the given iterator.
// Zero allocations.
func NewMVCCIterator(inner Iterator) *MVCCIterator {
	return &MVCCIterator{inner: inner}
}

// Next advances to the next non-deleted user key.
// Zero heap allocations in the hot path.
func (it *MVCCIterator) Next() bool {
	if it.ended || it.err != nil {
		return false
	}

	for {
		// Advance inner — either from peeked state or fresh
		var currentKey []byte
		if it.peeked {
			// We already have the next key from a previous scan.
			// Don't call inner.Next() — it's already positioned.
			it.peeked = false
			currentKey = it.inner.Key()
		} else {
			if !it.inner.Next() {
				it.ended = true
				return false
			}
			currentKey = it.inner.Key()
		}

		if currentKey == nil {
			continue
		}

		// Scan all versions of this user key.
		// The inner iterator produces entries sorted by (userKey, Timestamp)
		// where for the same user key, entries go from oldest to newest.
		// We track the newest non-deleted value.
		var bestValue []byte
		found := false
		hasTombstone := false
		versionCount := 0

		for {
			versionCount++
			if it.inner.IsDeleted() {
				// Tombstone for this user key. Note it but keep scanning: a
				// newer live version after an older tombstone must still be
				// returned (the key was re-inserted after deletion).
				hasTombstone = true
			} else {
				// Non-deleted version — update best value and reset the
				// tombstone flag. Since we iterate from oldest to newest,
				// this keeps the newest live version; a live version newer
				// than any tombstone means the key is live.
				bestValue = it.inner.Value()
				found = true
				hasTombstone = false
			}

			if !it.inner.Next() {
				it.ended = true
				break
			}
			nextKey := it.inner.Key()
			if !bytes.Equal(nextKey, currentKey) {
				// Different user key — save it for the next call.
				// inner is already positioned at this key.
				it.peeked = true
				break
			}
		}

		if found && !hasTombstone && bestValue != nil {
			it.key = currentKey
			it.value = bestValue
			return true
		}
		// Key was deleted (tombstone is the newest version) or no value found.
		// Continue to the next user key.
	}
}

// Key returns the current key.
func (it *MVCCIterator) Key() []byte {
	return it.key
}

// Value returns the current value.
func (it *MVCCIterator) Value() []byte {
	return it.value
}

// Err returns any error encountered during iteration.
func (it *MVCCIterator) Err() error {
	return it.err
}

// Close closes the underlying iterator.
func (it *MVCCIterator) Close() error {
	return it.inner.Close()
}

// IsDeleted returns false — MVCCIterator never returns deleted entries.
func (it *MVCCIterator) IsDeleted() bool {
	return false
}

// Ensure compile-time interface satisfaction.
var _ Iterator = (*MVCCIterator)(nil)
