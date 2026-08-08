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

package sstable

import (
	"encoding/binary"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// SSTableIterator iterates over key-value pairs in an SSTable.
// It reads blocks sequentially using the block index.
//
// On mmap platforms, block data is a slice of the mmap region (0 allocs).
// On fallback platforms, block data is from the sync.Pool.
//
// See: ARCH-05, PERF-03
type SSTableIterator struct {
	reader    *Reader
	blockIdx  int    // current block index
	blockData []byte // current block data
	pos       int    // current position within block
	key       mvcc.MVCCKey
	val       []byte
	err       error
	ended     bool
	isMmap    bool // true if reader uses mmap (no pool release needed)
}

// NewIterator creates a new SSTableIterator for this reader.
// Returns an error if the first block cannot be read.
//
// See: ARCH-05
func (r *Reader) NewIterator() (*SSTableIterator, error) {
	it := &SSTableIterator{
		reader:   r,
		blockIdx: -1,
		isMmap:   r.mmapFile.Data() != nil,
	}
	// Advance to the first block
	if !it.nextBlock() {
		if it.err != nil {
			return nil, it.err
		}
		// Empty SSTable — valid state
		it.ended = true
	}
	return it, nil
}

// Next advances the iterator to the next key-value pair.
// Returns false when exhausted or on error.
func (it *SSTableIterator) Next() bool {
	if it.ended || it.err != nil {
		return false
	}

	for {
		// Try to read next entry from current block
		if it.pos < len(it.blockData) {
			if it.pos+8 > len(it.blockData) {
				it.err = ErrCorrupted
				return false
			}

			keyLen := binary.LittleEndian.Uint32(it.blockData[it.pos:])
			valLen := binary.LittleEndian.Uint32(it.blockData[it.pos+4:])

			if it.pos+8+int(keyLen)+int(valLen) > len(it.blockData) {
				it.err = ErrCorrupted
				return false
			}

			entryKey := it.blockData[it.pos+8 : it.pos+8+int(keyLen)]
			entryVal := it.blockData[it.pos+8+int(keyLen) : it.pos+8+int(keyLen)+int(valLen)]
			it.pos += 8 + int(keyLen) + int(valLen)

			mvccKey, err := decodeMVCCKey(entryKey)
			if err != nil {
				it.err = err
				return false
			}

			// decodeMVCCKey returns mvccKey.Key as a slice into entryKey, which
			// itself is a slice into blockData. blockData may come from the
			// sync.Pool (released in nextBlock/Close) or from the mmap region
			// (unmapped when the Reader is closed). If the caller holds the key
			// beyond the current block, it becomes a use-after-free. The value is
			// already deep-copied below, so deep-copy the key the same way.
			keyCopy := make([]byte, len(mvccKey.Key))
			copy(keyCopy, mvccKey.Key)
			it.key = mvcc.MVCCKey{Key: keyCopy, Timestamp: mvccKey.Timestamp}

			// Copy value since blockData may be from mmap or pool
			val := make([]byte, len(entryVal))
			copy(val, entryVal)
			// Strip the leading type tag (v0.4+). Legacy values have no tag and
			// are passed through as-is.
			if len(val) > 0 && isValidValueTag(val[0]) {
				val = val[1:]
			}
			it.val = val
			return true
		}

		// Current block exhausted, move to next
		if !it.nextBlock() {
			return false
		}
	}
}

// nextBlock advances to the next block and reads its data.
func (it *SSTableIterator) nextBlock() bool {
	it.blockIdx++
	if it.blockIdx >= len(it.reader.indexEntries) {
		it.ended = true
		return false
	}

	// Release previous block data (only if from pool, not mmap)
	if it.blockData != nil && !it.isMmap {
		ReleaseBlock(it.blockData)
		it.blockData = nil
	}

	blockOffset := it.reader.indexEntries[it.blockIdx].offset
	data, err := it.reader.readBlock(blockOffset)
	if err != nil {
		it.err = err
		return false
	}
	it.blockData = data
	it.pos = 0
	return true
}

// Key returns the current key.
func (it *SSTableIterator) Key() mvcc.MVCCKey {
	return it.key
}

// Value returns the current value.
func (it *SSTableIterator) Value() []byte {
	return it.val
}

// Err returns any error encountered during iteration.
func (it *SSTableIterator) Err() error {
	return it.err
}

// Close releases resources held by the iterator.
func (it *SSTableIterator) Close() {
	if it.blockData != nil && !it.isMmap {
		ReleaseBlock(it.blockData)
		it.blockData = nil
	}
	it.ended = true
}
