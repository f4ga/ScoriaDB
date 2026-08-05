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
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"sort"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

const (
	// BlockSize is the data block size (16 KB)
	BlockSize = 16 * 1024
	// BloomFilterBitsPerKey is the number of bits per key for Bloom filter (false positive ~0.01)
	BloomFilterBitsPerKey = 10
	// MagicNumber is the magic number in the footer
	MagicNumber = 0x53434F5249415F53 // "SCORIA_S" in ASCII
)

// rawEntry is a key-value pair stored in memory before sorting and block formation.
type rawEntry struct {
	key   mvcc.MVCCKey
	value []byte
}

// Writer writes SSTable to file.
//
// All entries are buffered in memory and sorted by encoded MVCC key during Finish().
// This ensures the SSTable is correctly sorted even when keys are appended in
// non-sorted order (e.g., after user key wrap-around with 2-byte keys >65536 entries).
//
// Blocks are formed from the sorted entries and written to disk in order.
// The index stores the encoded MVCC key of the first entry in each block.
//
// See: PROMPT-SSTABLE-FINAL
type Writer struct {
	file   *os.File
	writer *bufio.Writer

	entries []rawEntry

	bloomFilter *BloomFilter
	minKey      []byte
	maxKey      []byte
}

// NewWriter creates a new Writer for writing SSTable.
// expectedKeys is the expected number of keys for Bloom filter sizing.
// If expectedKeys <= 0, a default size is used.
func NewWriter(path string, expectedKeys int) (*Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSTable file: %w", err)
	}
	writer := bufio.NewWriter(file)

	// Pre-allocate entries slice if expectedKeys is reasonable
	entryCap := expectedKeys
	if entryCap <= 0 || entryCap > 1000000 {
		entryCap = 1024
	}

	return &Writer{
		file:        file,
		writer:      writer,
		entries:     make([]rawEntry, 0, entryCap),
		bloomFilter: NewBloomFilter(expectedKeys),
	}, nil
}

// Append adds a key-value pair to the SSTable.
// The entry is buffered in memory. Actual sorting and block formation
// happens in Finish().
func (w *Writer) Append(key mvcc.MVCCKey, value []byte) error {
	// Add key to Bloom filter
	w.bloomFilter.Add(key.Key)

	// Update min/max keys
	if w.minKey == nil || keys.CompareKeys(key.Key, w.minKey) < 0 {
		w.minKey = key.Key
	}
	if w.maxKey == nil || keys.CompareKeys(key.Key, w.maxKey) > 0 {
		w.maxKey = key.Key
	}

	// Buffer entry in memory
	w.entries = append(w.entries, rawEntry{key: key, value: value})
	return nil
}

// Finish completes the SSTable write.
//
// Steps:
// 1. Sort all entries by encoded MVCC key
// 2. Form blocks from sorted entries
// 3. Write blocks to disk
// 4. Write index, Bloom filter, range keys, footer
//
// See: PROMPT-SSTABLE-FINAL
func (w *Writer) Finish() error {
	if len(w.entries) == 0 {
		// Write empty footer for empty SSTable
		return w.writeFooter(0, nil, nil, nil, nil)
	}

	// Sort all entries by (user key, inverted timestamp).
	// Using keys.CompareKeys for user key comparison ensures correct
	// lexicographic ordering regardless of key length. Sorting by encoded
	// MVCCKey bytes is WRONG because encodeMVCCKey prepends the key length
	// as a 4-byte uint32, which would sort by key length first, not by content.
	// See: BUG-SORT-01
	sort.Slice(w.entries, func(i, j int) bool {
		ki, kj := w.entries[i].key, w.entries[j].key
		if cmp := keys.CompareKeys(ki.Key, kj.Key); cmp != 0 {
			return cmp < 0
		}
		// Same user key: newer version (larger inverted timestamp) first
		return ki.Timestamp > kj.Timestamp
	})

	// Form blocks from sorted entries
	type block struct {
		firstKey []byte // raw user key of first entry (for binary search in index)
		data     []byte // serialized entries
	}
	var blocks []block
	var currentBlock []byte
	var blockFirstKey []byte

	for _, entry := range w.entries {
		keyBytes := encodeMVCCKey(entry.key)
		entryBytes := encodeEntry(keyBytes, entry.value)

		// If current block would overflow, flush it
		if len(currentBlock)+len(entryBytes) > BlockSize && len(currentBlock) > 0 {
			blocks = append(blocks, block{
				firstKey: blockFirstKey,
				data:     currentBlock,
			})
			currentBlock = nil
			blockFirstKey = nil
		}

		if blockFirstKey == nil {
			blockFirstKey = append([]byte(nil), entry.key.Key...) // raw user key
		}
		currentBlock = append(currentBlock, entryBytes...)
	}

	// Flush last block
	if len(currentBlock) > 0 {
		blocks = append(blocks, block{
			firstKey: blockFirstKey,
			data:     currentBlock,
		})
	}

	// Write blocks to disk and build index
	indexEntries := make([][]byte, 0, len(blocks))
	indexOffsets := make([]uint64, 0, len(blocks))
	var offset uint64

	for _, blk := range blocks {
		// Index stores raw user key (without timestamp) for binary search
		indexEntries = append(indexEntries, blk.firstKey)
		indexOffsets = append(indexOffsets, offset)

		// Write block: [blockSize:4][blockData:blockSize][CRC32:4]
		blockSize := uint32(len(blk.data))
		if err := binary.Write(w.writer, binary.LittleEndian, blockSize); err != nil {
			return fmt.Errorf("failed to write block size: %w", err)
		}
		if _, err := w.writer.Write(blk.data); err != nil {
			return fmt.Errorf("failed to write block data: %w", err)
		}
		crc := crc32.ChecksumIEEE(blk.data)
		var crcBuf [4]byte
		binary.LittleEndian.PutUint32(crcBuf[:], crc)
		if _, err := w.writer.Write(crcBuf[:]); err != nil {
			return fmt.Errorf("failed to write block CRC: %w", err)
		}
		offset += 4 + uint64(len(blk.data)) + 4
	}

	return w.writeFooter(offset, indexEntries, indexOffsets, w.minKey, w.maxKey)
}

// writeFooter writes index, Bloom filter, range keys, and footer.
func (w *Writer) writeFooter(
	offset uint64,
	indexEntries [][]byte,
	indexOffsets []uint64,
	minKey, maxKey []byte,
) error {
	// Write block index
	indexStart := offset
	for i, key := range indexEntries {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(key))); err != nil {
			return err
		}
		offset += 4
		if _, err := w.writer.Write(key); err != nil {
			return err
		}
		offset += uint64(len(key))
		if err := binary.Write(w.writer, binary.LittleEndian, indexOffsets[i]); err != nil {
			return err
		}
		offset += 8
	}
	indexSize := offset - indexStart

	// Write Bloom filter
	bloomStart := offset
	bloomBytes := w.bloomFilter.Encode()
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(bloomBytes))); err != nil {
		return err
	}
	offset += 4
	if _, err := w.writer.Write(bloomBytes); err != nil {
		return err
	}
	offset += uint64(len(bloomBytes))
	bloomSize := offset - bloomStart

	// Write min key
	minKeyStart := offset
	if minKey != nil {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(minKey))); err != nil {
			return err
		}
		if _, err := w.writer.Write(minKey); err != nil {
			return err
		}
		offset += 4 + uint64(len(minKey))
	} else {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(0)); err != nil {
			return err
		}
		offset += 4
	}

	// Write max key
	maxKeyStart := offset
	if maxKey != nil {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(maxKey))); err != nil {
			return err
		}
		if _, err := w.writer.Write(maxKey); err != nil {
			return err
		}
		//nolint:ineffassign // offset is tracked for future footer extensions
		offset += 4 + uint64(len(maxKey))
	} else {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(0)); err != nil {
			return err
		}
		//nolint:ineffassign // offset is tracked for future footer extensions
		offset += 4
	}

	// Write footer
	footer := Footer{
		IndexOffset:  indexStart,
		IndexSize:    indexSize,
		BloomOffset:  bloomStart,
		BloomSize:    bloomSize,
		NumKeys:      uint64(len(w.entries)),
		Magic:        MagicNumber,
		MinKeyOffset: minKeyStart,
		MinKeyLength: uint64(len(minKey)),
		MaxKeyOffset: maxKeyStart,
		MaxKeyLength: uint64(len(maxKey)),
	}
	if err := binary.Write(w.writer, binary.LittleEndian, footer); err != nil {
		return fmt.Errorf("failed to write footer: %w", err)
	}

	// Flush buffered data to disk
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}

	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close SSTable file: %w", err)
	}
	return nil
}

// encodeEntry encodes a key-value pair into bytes.
func encodeEntry(key, value []byte) []byte {
	kl := len(key)
	vl := len(value)
	buf := make([]byte, 4+4+kl+vl)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(kl))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(vl))
	copy(buf[8:8+kl], key)
	copy(buf[8+kl:], value)
	return buf
}

// Footer represents the SSTable footer.
type Footer struct {
	IndexOffset  uint64
	IndexSize    uint64
	BloomOffset  uint64
	BloomSize    uint64
	NumKeys      uint64
	Magic        uint64
	MinKeyOffset uint64
	MinKeyLength uint64
	MaxKeyOffset uint64
	MaxKeyLength uint64
}
