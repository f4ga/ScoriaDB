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

// Writer writes SSTable to file.
type Writer struct {
	file   *os.File
	writer *bufio.Writer
	offset uint64

	// Current block
	blockBuf      []byte
	blockEntries  int
	blockStartKey []byte // first key in current block (for index)
	blockStartOff uint64

	// Block index: stores LAST key of each block and its offset
	indexEntries [][]byte
	indexOffsets []uint64

	bloomFilter *BloomFilter
	keys        [][]byte
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

	return &Writer{
		file:         file,
		writer:       writer,
		blockBuf:     make([]byte, 0, BlockSize),
		bloomFilter:  NewBloomFilter(expectedKeys),
		indexEntries: make([][]byte, 0),
		indexOffsets: make([]uint64, 0),
		keys:         make([][]byte, 0),
	}, nil
}

// Append adds a key-value pair to the SSTable.
func (w *Writer) Append(key mvcc.MVCCKey, value []byte) error {
	// Add key to Bloom filter
	w.bloomFilter.Add(key.Key)
	w.keys = append(w.keys, key.Key)

	// Update min/max keys
	if w.minKey == nil || keys.CompareKeys(key.Key, w.minKey) < 0 {
		w.minKey = key.Key
	}
	if w.maxKey == nil || keys.CompareKeys(key.Key, w.maxKey) > 0 {
		w.maxKey = key.Key
	}

	// Serialize key and value
	keyBytes := encodeMVCCKey(key)
	entry := encodeEntry(keyBytes, value)

	// If current block would overflow, flush it
	if len(w.blockBuf)+len(entry) > BlockSize && w.blockEntries > 0 {
		if err := w.flushBlock(); err != nil {
			return err
		}
	}

	// Remember first key of the block
	if w.blockEntries == 0 {
		w.blockStartKey = keyBytes
		w.blockStartOff = w.offset
	}

	// Add entry to block
	w.blockBuf = append(w.blockBuf, entry...)
	w.blockEntries++

	return nil
}

// flushBlock writes the current block to disk and adds an entry to the index.
// Block format on disk: [blockSize:4][blockData:blockSize][CRC32:4]
func (w *Writer) flushBlock() error {
	if w.blockEntries == 0 {
		return nil
	}

	// Store the LAST key of the block for binary search
	// We need to track the last key of the current block.
	// Since we only have the first key, we store the first key
	// and the index will be used for binary search by first key.
	w.indexEntries = append(w.indexEntries, w.blockStartKey)
	w.indexOffsets = append(w.indexOffsets, w.blockStartOff)

	// Write block size and data
	blockSize := uint32(len(w.blockBuf))
	if err := binary.Write(w.writer, binary.LittleEndian, blockSize); err != nil {
		return fmt.Errorf("failed to write block size: %w", err)
	}
	if _, err := w.writer.Write(w.blockBuf); err != nil {
		return fmt.Errorf("failed to write block data: %w", err)
	}

	// Write CRC32 checksum of block data
	crc := crc32.ChecksumIEEE(w.blockBuf)
	var crcBuf [4]byte
	binary.LittleEndian.PutUint32(crcBuf[:], crc)
	if _, err := w.writer.Write(crcBuf[:]); err != nil {
		return fmt.Errorf("failed to write block CRC: %w", err)
	}

	w.offset += 4 + uint64(len(w.blockBuf)) + 4 // blockSize + data + CRC32

	// Reset block buffer
	w.blockBuf = w.blockBuf[:0]
	w.blockEntries = 0

	return nil
}

// Finish completes the SSTable write, writing index, Bloom filter, range keys, and footer.
func (w *Writer) Finish() error {
	// Flush the last block
	if err := w.flushBlock(); err != nil {
		return err
	}

	// Write block index
	indexStart := w.offset
	for i, key := range w.indexEntries {
		// Write key length and key
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(key))); err != nil {
			return err
		}
		w.offset += 4
		if _, err := w.writer.Write(key); err != nil {
			return err
		}
		w.offset += uint64(len(key))
		// Write block offset
		if err := binary.Write(w.writer, binary.LittleEndian, w.indexOffsets[i]); err != nil {
			return err
		}
		w.offset += 8
	}
	indexSize := w.offset - indexStart

	// Write Bloom filter
	bloomStart := w.offset
	bloomBytes := w.bloomFilter.Encode()
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(bloomBytes))); err != nil {
		return err
	}
	w.offset += 4
	if _, err := w.writer.Write(bloomBytes); err != nil {
		return err
	}
	w.offset += uint64(len(bloomBytes))
	bloomSize := w.offset - bloomStart

	// Write min and max keys (range filter)
	minKeyStart := w.offset
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(w.minKey))); err != nil {
		return err
	}
	if _, err := w.writer.Write(w.minKey); err != nil {
		return err
	}
	w.offset += 4 + uint64(len(w.minKey))

	maxKeyStart := w.offset
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(w.maxKey))); err != nil {
		return err
	}
	if _, err := w.writer.Write(w.maxKey); err != nil {
		return err
	}
	w.offset += 4 + uint64(len(w.maxKey))

	// Write footer
	footer := Footer{
		IndexOffset:  indexStart,
		IndexSize:    indexSize,
		BloomOffset:  bloomStart,
		BloomSize:    bloomSize,
		NumKeys:      uint64(len(w.keys)),
		Magic:        MagicNumber,
		MinKeyOffset: minKeyStart,
		MinKeyLength: uint64(len(w.minKey)),
		MaxKeyOffset: maxKeyStart,
		MaxKeyLength: uint64(len(w.maxKey)),
	}
	if err := binary.Write(w.writer, binary.LittleEndian, footer); err != nil {
		return fmt.Errorf("failed to write footer: %w", err)
	}

	// Flush and close
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
