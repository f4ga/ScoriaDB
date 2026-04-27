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
	"io"
	"scoriadb/internal/mvcc"
)

// SSTableIterator iterates over all key-value pairs in an SSTable.
// It loads all entries into memory for simplicity.
type SSTableIterator struct {
	entries []kvEntry
	index   int
}

type kvEntry struct {
	key   mvcc.MVCCKey
	value []byte
}

// NewIterator creates an iterator over the entire SSTable.
// It reads all blocks and decodes all entries.
func (r *Reader) NewIterator() (*SSTableIterator, error) {
	var entries []kvEntry

	// Iterate over all blocks
	for _, idxEntry := range r.indexEntries {
		// Seek to block start
		if _, err := r.file.Seek(int64(idxEntry.offset), 0); err != nil {
			return nil, err
		}
		var blockSize uint32
		if err := binary.Read(r.file, binary.LittleEndian, &blockSize); err != nil {
			return nil, err
		}
		blockData := make([]byte, blockSize)
		if _, err := io.ReadFull(r.file, blockData); err != nil {
			return nil, err
		}

		// Parse entries in block
		pos := 0
		for pos < len(blockData) {
			keyLen := binary.LittleEndian.Uint32(blockData[pos:])
			valLen := binary.LittleEndian.Uint32(blockData[pos+4:])
			entryKey := blockData[pos+8 : pos+8+int(keyLen)]
			entryVal := blockData[pos+8+int(keyLen) : pos+8+int(keyLen)+int(valLen)]
			pos += 8 + int(keyLen) + int(valLen)

			mvccKey, err := decodeMVCCKey(entryKey)
			if err != nil {
				// Skip corrupted entry
				continue
			}
			entries = append(entries, kvEntry{
				key:   mvccKey,
				value: entryVal,
			})
		}
	}

	return &SSTableIterator{
		entries: entries,
		index:   -1,
	}, nil
}

// Next advances the iterator to the next entry.
func (it *SSTableIterator) Next() bool {
	it.index++
	return it.index < len(it.entries)
}

// Key returns the current key.
func (it *SSTableIterator) Key() mvcc.MVCCKey {
	return it.entries[it.index].key
}

// Value returns the current value.
func (it *SSTableIterator) Value() []byte {
	return it.entries[it.index].value
}

// Close releases resources.
func (it *SSTableIterator) Close() {
	it.entries = nil
}