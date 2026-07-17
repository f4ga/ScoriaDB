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
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// SSTableIterator iterates over key-value pairs in an SSTable.
// It reads blocks sequentially using the block index.
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
}

// NewIterator creates a new SSTableIterator for this reader.
// Returns an error if the first block cannot be read.
// See: ARCH-05
func (r *Reader) NewIterator() (*SSTableIterator, error) {
	it := &SSTableIterator{
		reader:   r,
		blockIdx: -1,
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

			it.key = mvccKey
			// Copy value since blockData will be reused
			val := make([]byte, len(entryVal))
			copy(val, entryVal)
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

	// Release previous block data
	if it.blockData != nil {
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
	if it.blockData != nil {
		ReleaseBlock(it.blockData)
		it.blockData = nil
	}
	it.ended = true
}

// blockPool is a sync.Pool for reusing block buffers during SSTable reads.
// Stores *[]byte to satisfy staticcheck (sync.Pool with pointer types).
var blockPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 4096)
		return &buf
	},
}

// Reader reads SSTable from file.
type Reader struct {
	file         *os.File
	footer       Footer
	indexEntries []IndexEntry
	bloomFilter  *BloomFilter
	minKey       []byte
	maxKey       []byte
}

// IndexEntry represents a block index entry.
type IndexEntry struct {
	key    []byte // encoded MVCCKey (first key in block)
	offset uint64
}

// Open opens SSTable for reading.
func Open(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SSTable file: %w", err)
	}

	if _, err := file.Seek(-80, io.SeekEnd); err != nil {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("failed to seek to footer: %w", err)
	}
	var footer Footer
	if err := binary.Read(file, binary.LittleEndian, &footer); err != nil {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("failed to read footer: %w", err)
	}
	if footer.Magic != MagicNumber {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("invalid SSTable magic number")
	}

	if _, err := file.Seek(int64(footer.IndexOffset), io.SeekStart); err != nil {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("failed to seek to index: %w", err)
	}
	indexEntries := make([]IndexEntry, 0)
	remaining := footer.IndexSize
	for remaining > 0 {
		var keyLen uint32
		if err := binary.Read(file, binary.LittleEndian, &keyLen); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read key length: %w", err)
		}
		remaining -= 4
		key := make([]byte, keyLen)
		if _, err := io.ReadFull(file, key); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read key: %w", err)
		}
		remaining -= uint64(keyLen)
		var offset uint64
		if err := binary.Read(file, binary.LittleEndian, &offset); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read block offset: %w", err)
		}
		remaining -= 8
		indexEntries = append(indexEntries, IndexEntry{key: key, offset: offset})
	}

	if _, err := file.Seek(int64(footer.BloomOffset), io.SeekStart); err != nil {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("failed to seek to bloom filter: %w", err)
	}
	var bloomSize uint32
	if err := binary.Read(file, binary.LittleEndian, &bloomSize); err != nil {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("failed to read bloom filter size: %w", err)
	}
	bloomBytes := make([]byte, bloomSize)
	if _, err := io.ReadFull(file, bloomBytes); err != nil {
		errors.CloseWithLog(file, "sstable-file")
		return nil, fmt.Errorf("failed to read bloom filter: %w", err)
	}
	bloomFilter := DecodeBloomFilter(bloomBytes, 3)

	var minKey []byte
	if footer.MinKeyLength > 0 {
		if _, err := file.Seek(int64(footer.MinKeyOffset), io.SeekStart); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to seek to min key: %w", err)
		}
		var keyLen uint32
		if err := binary.Read(file, binary.LittleEndian, &keyLen); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read min key length: %w", err)
		}
		minKey = make([]byte, keyLen)
		if _, err := io.ReadFull(file, minKey); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read min key: %w", err)
		}
	}

	var maxKey []byte
	if footer.MaxKeyLength > 0 {
		if _, err := file.Seek(int64(footer.MaxKeyOffset), io.SeekStart); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to seek to max key: %w", err)
		}
		var keyLen uint32
		if err := binary.Read(file, binary.LittleEndian, &keyLen); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read max key length: %w", err)
		}
		maxKey = make([]byte, keyLen)
		if _, err := io.ReadFull(file, maxKey); err != nil {
			errors.CloseWithLog(file, "sstable-file")
			return nil, fmt.Errorf("failed to read max key: %w", err)
		}
	}

	return &Reader{
		file:         file,
		footer:       footer,
		indexEntries: indexEntries,
		bloomFilter:  bloomFilter,
		minKey:       minKey,
		maxKey:       maxKey,
	}, nil
}

// readBlock reads a data block from the SSTable at the given offset.
func (r *Reader) readBlock(offset uint64) ([]byte, error) {
	if _, err := r.file.Seek(int64(offset), io.SeekStart); err != nil {
		return nil, err
	}
	var blockSize uint32
	if err := binary.Read(r.file, binary.LittleEndian, &blockSize); err != nil {
		return nil, err
	}

	bufPtr, ok := blockPool.Get().(*[]byte)
	if !ok {
		buf := make([]byte, blockSize)
		if _, err := io.ReadFull(r.file, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	buf := *bufPtr
	if cap(buf) < int(blockSize) {
		buf = make([]byte, blockSize)
	} else {
		buf = buf[:blockSize]
	}

	if _, err := io.ReadFull(r.file, buf); err != nil {
		blockPool.Put(&buf)
		return nil, err
	}
	return buf, nil
}

// ReleaseBlock returns a buffer to the pool.
func ReleaseBlock(buf []byte) {
	blockPool.Put(&buf)
}

// Lookup searches for a key in the SSTable and returns the value if found.
// Lookup searches for a key in the SSTable and returns the value if found.
func (r *Reader) Lookup(key mvcc.MVCCKey) ([]byte, bool) {
	userKey := key.Key

	// Range filter: if key is outside min/max, return false
	if len(r.minKey) > 0 && bytes.Compare(userKey, r.minKey) < 0 {
		return nil, false
	}
	if len(r.maxKey) > 0 && bytes.Compare(userKey, r.maxKey) > 0 {
		return nil, false
	}

	// Bloom filter: if key is definitely not present, return false
	if !r.bloomFilter.MayContain(userKey) {
		return nil, false
	}

	// Find block using binary search on index
	// Index stores the first key of each block
	// We need to find the block where first_key <= userKey
	blockIndex := -1
	for i, entry := range r.indexEntries {
		idxKey, err := decodeMVCCKey(entry.key)
		if err != nil {
			continue
		}
		// If this block's first key > userKey, the key belongs to the previous block
		if keys.CompareKeys(idxKey.Key, userKey) > 0 {
			break
		}
		blockIndex = i
	}

	// If no block found (key is before first block), return false
	if blockIndex < 0 {
		return nil, false
	}

	// Read the block
	blockOffset := r.indexEntries[blockIndex].offset
	blockData, err := r.readBlock(blockOffset)
	if err != nil {
		return nil, false
	}
	defer ReleaseBlock(blockData)

	// Search for the key in the block
	pos := 0
	for pos < len(blockData) {
		if pos+8 > len(blockData) {
			break
		}

		keyLen := binary.LittleEndian.Uint32(blockData[pos:])
		valLen := binary.LittleEndian.Uint32(blockData[pos+4:])

		if pos+8+int(keyLen)+int(valLen) > len(blockData) {
			break
		}

		entryKey := blockData[pos+8 : pos+8+int(keyLen)]
		entryVal := blockData[pos+8+int(keyLen) : pos+8+int(keyLen)+int(valLen)]
		pos += 8 + int(keyLen) + int(valLen)

		mvccKey, err := decodeMVCCKey(entryKey)
		if err != nil {
			continue
		}

		cmp := keys.CompareKeys(mvccKey.Key, userKey)

		// Key found
		if cmp == 0 {
			// Check version visibility
			if mvccKey.Timestamp >= key.Timestamp {
				if len(entryVal) == 0 {
					return nil, false // tombstone
				}
				// Copy value before returning (blockData will be released)
				val := make([]byte, len(entryVal))
				copy(val, entryVal)
				return val, true
			}
		} else if cmp > 0 {
			// Since the block is sorted, if we've passed the key, it's not here
			break
		}
	}

	return nil, false
}

// Close closes the SSTable file.
func (r *Reader) Close() error {
	return r.file.Close()
}
