// Copyright 2026 Ekaterina Godulyan
// Licensed under the Apache License, Version 2.0.

package sstable

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"sort"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

const (
	// BlockSize defines the target size of each data block (16 KB).
	// This balances read amplification and index size.
	BlockSize = 16 * 1024

	// BloomFilterBitsPerKey sets the bits per key for Bloom filter
	// (yielding ~1% false positive rate).
	BloomFilterBitsPerKey = 10

	// MagicNumber is a 64-bit identifier written in the footer.
	MagicNumber = 0x53434F5249415F53 // "SCORIA_S" in ASCII
)

// rawEntry represents a key-value pair before sorting/block formation.
type rawEntry struct {
	key   mvcc.MVCCKey
	value []byte
	// tagged reports that value is already in the tagged storage format
	// (leading type tag + payload) and must be written verbatim, without the
	// automatic inline-tagging applied by encodeEntry. Used by AppendTagged
	// to preserve ValuePointer semantics across flush. See: WIS-KEY-01.
	tagged bool
}

// Writer builds an SSTable file from a set of key-value pairs.
// All entries are buffered in memory, sorted by (user key, inverted timestamp),
// and then written to disk in blocks.
//
// CRITICAL INVARIANT:
//
//	For each block, the index key stored in the footer MUST be the SMALLEST
//	key in that block. This invariant is required for binary search to work.
//
// The writer guarantees this by sorting entries globally and then forming
// blocks; after a block is filled, the first entry of that block is used
// as the index key.
type Writer struct {
	file   *os.File
	writer *bufio.Writer

	entries []rawEntry   // all buffered entries (unsorted)
	bloom   *BloomFilter // adaptive Bloom filter
	minKey  []byte       // smallest user key in the entire SSTable
	maxKey  []byte       // largest user key in the entire SSTable
}

// NewWriter creates a new Writer for the given file path.
// expectedKeys hints the number of keys for Bloom filter sizing.
func NewWriter(path string, expectedKeys int) (*Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSTable file: %w", err)
	}
	writer := bufio.NewWriter(file)

	// Pre-allocate entries slice to avoid reallocations.
	entryCap := expectedKeys
	if entryCap <= 0 || entryCap > 1_000_000 {
		entryCap = 1024
	}

	return &Writer{
		file:    file,
		writer:  writer,
		entries: make([]rawEntry, 0, entryCap),
		bloom:   NewBloomFilter(expectedKeys),
	}, nil
}

// Append adds a key-value pair to the SSTable.
// The entry is stored in memory; actual sorting and writing happen in Finish().
// The value is expected in the UNtagged form and will be tagged as inline by
// encodeEntry during Finish().
func (w *Writer) Append(key mvcc.MVCCKey, value []byte) error {
	return w.appendInternal(key, value, false)
}

// AppendTagged adds a key-value pair whose value is already in the tagged
// storage format (leading type tag + payload). The value is written verbatim,
// preserving its tag so the reader can resolve a TypeValuePointer through the
// VLog. See: WIS-KEY-01.
func (w *Writer) AppendTagged(key mvcc.MVCCKey, value []byte) error {
	return w.appendInternal(key, value, true)
}

func (w *Writer) appendInternal(key mvcc.MVCCKey, value []byte, tagged bool) error {
	// Update Bloom filter with the raw user key (without timestamp).
	w.bloom.Add(key.Key)

	// Track min/max keys for range filtering.
	if w.minKey == nil || keys.CompareKeys(key.Key, w.minKey) < 0 {
		w.minKey = append([]byte{}, key.Key...)
	}
	if w.maxKey == nil || keys.CompareKeys(key.Key, w.maxKey) > 0 {
		w.maxKey = append([]byte{}, key.Key...)
	}

	// Store the entry in the buffer.
	w.entries = append(w.entries, rawEntry{key: key, value: value, tagged: tagged})
	return nil
}

// Finish completes the SSTable writing process.
// Steps:
//  1. Sort all entries by (user key, inverted timestamp).
//  2. Form data blocks of size <= BlockSize.
//  3. Write each block to disk, computing its CRC32.
//  4. Build index (first key of each block) and Bloom filter.
//  5. Write index, Bloom filter, and footer.
//
// The index keys are guaranteed to be the SMALLEST key in each block,
// maintaining the invariant required for binary search.
func (w *Writer) Finish() error {
	if len(w.entries) == 0 {
		// Empty SSTable: write an empty footer.
		return w.writeFooter(0, nil, nil, nil, nil)
	}

	// --------------------------------------------------------------------
	// STEP 1: Sort all entries by (user key, inverted timestamp).
	// --------------------------------------------------------------------
	// Using keys.CompareKeys ensures correct lexicographic order.
	// For equal user keys, newer versions (larger timestamp) come first
	// so that Lookup with snapshot timestamp sees the newest visible version.
	sort.Slice(w.entries, func(i, j int) bool {
		ki, kj := w.entries[i].key, w.entries[j].key
		if cmp := keys.CompareKeys(ki.Key, kj.Key); cmp != 0 {
			return cmp < 0
		}
		// Same user key: newer version (larger inverted timestamp) first.
		return ki.Timestamp > kj.Timestamp
	})

	// --------------------------------------------------------------------
	// STEP 2: Form data blocks from sorted entries.
	// --------------------------------------------------------------------
	// We maintain a temporary slice `blockEntries` that holds the entries
	// of the current block. After the block is finalized, we use the first
	// entry (which is the smallest key) as the index key.
	type block struct {
		firstKey []byte // raw user key of the first (smallest) entry
		data     []byte // serialized block data (excluding size and CRC)
	}
	var blocks []block
	var currentBlock []byte
	var blockEntries []rawEntry // entries in the current block

	for _, entry := range w.entries {
		// Encode the MVCC key and the value (with type tag).
		keyBytes := encodeMVCCKey(entry.key)
		var entryBytes []byte
		if entry.tagged {
			// Value is already in tagged storage format; write it verbatim so
			// its tag (e.g. TypeValuePointer) survives to the reader.
			// See: WIS-KEY-01.
			entryBytes = encodeEntryTagged(keyBytes, entry.value)
		} else {
			entryBytes = encodeEntry(keyBytes, entry.value)
		}

		// If adding this entry would exceed the block size, finalize the block.
		if len(currentBlock)+len(entryBytes) > BlockSize && len(currentBlock) > 0 {
			// CRITICAL: firstKey is the FIRST entry in blockEntries,
			// which is the SMALLEST key because entries are sorted globally.
			//
			// We MUST copy the key because entry.key.Key is a slice that may
			// reference the arena (or caller-owned memory). The arena can be
			// overwritten or released before the index is serialized, so we
			// take an owned copy here. See: SSTABLE-INDEX-01
			firstKeySrc := blockEntries[0].key.Key
			firstKey := make([]byte, len(firstKeySrc))
			copy(firstKey, firstKeySrc)
			blocks = append(blocks, block{
				firstKey: firstKey,
				data:     currentBlock,
			})
			currentBlock = nil
			blockEntries = nil
		}

		// Add entry to the current block.
		if blockEntries == nil {
			blockEntries = make([]rawEntry, 0, 64) // pre-allocate
		}
		blockEntries = append(blockEntries, entry)
		currentBlock = append(currentBlock, entryBytes...)
	}

	// Finalize the last block.
	if len(currentBlock) > 0 {
		// Copy the key to own the memory (see comment above).
		firstKeySrc := blockEntries[0].key.Key // always the smallest
		firstKey := make([]byte, len(firstKeySrc))
		copy(firstKey, firstKeySrc)
		blocks = append(blocks, block{
			firstKey: firstKey,
			data:     currentBlock,
		})
	}

	// Diagnostic: report the number of entries and blocks before writing so
	// data loss between the MemTable iterator and the SSTable writer can be
	// localized (data loss in the iterator vs. the writer). See: SST-03.
	logger.Info("SSTable writer: finishing %d entries, %d blocks", len(w.entries), len(blocks))

	// --------------------------------------------------------------------
	// STEP 3: Write blocks to disk and build index.
	// --------------------------------------------------------------------
	indexEntries := make([][]byte, 0, len(blocks))
	indexOffsets := make([]uint64, 0, len(blocks))
	var offset uint64 // current file offset

	for _, blk := range blocks {
		// Store the index key (raw user key, no timestamp).
		indexEntries = append(indexEntries, blk.firstKey)
		indexOffsets = append(indexOffsets, offset)

		// Write block format: [blockSize:4][blockData:blockSize][CRC32:4]
		blockSize := uint32(len(blk.data))
		if err := binary.Write(w.writer, binary.LittleEndian, blockSize); err != nil {
			return fmt.Errorf("failed to write block size: %w", err)
		}
		if _, err := w.writer.Write(blk.data); err != nil {
			return fmt.Errorf("failed to write block data: %w", err)
		}
		crc := crc32.ChecksumIEEE(blk.data)
		if err := binary.Write(w.writer, binary.LittleEndian, crc); err != nil {
			return fmt.Errorf("failed to write block CRC: %w", err)
		}
		offset += 4 + uint64(len(blk.data)) + 4
	}

	// --------------------------------------------------------------------
	// STEP 4: Write index, Bloom filter, min/max keys, and footer.
	// --------------------------------------------------------------------
	return w.writeFooter(offset, indexEntries, indexOffsets, w.minKey, w.maxKey)
}

// writeFooter writes the index, Bloom filter, min/max keys, and footer.
func (w *Writer) writeFooter(
	offset uint64,
	indexEntries [][]byte,
	indexOffsets []uint64,
	minKey, maxKey []byte,
) error {
	// Write block index: for each block, store (keyLen, key, offset).
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

	// Write Bloom filter: [bloomSize:4][bloomData:bloomSize].
	bloomStart := offset
	bloomBytes := w.bloom.Encode()
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(bloomBytes))); err != nil {
		return err
	}
	offset += 4
	if _, err := w.writer.Write(bloomBytes); err != nil {
		return err
	}
	offset += uint64(len(bloomBytes))
	bloomSize := offset - bloomStart

	// Write min key: [keyLen:4][key:keyLen].
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

	// Write max key: [keyLen:4][key:keyLen].
	maxKeyStart := offset
	if maxKey != nil {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(maxKey))); err != nil {
			return err
		}
		if _, err := w.writer.Write(maxKey); err != nil {
			return err
		}
		offset += 4 + uint64(len(maxKey))
	} else {
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(0)); err != nil {
			return err
		}
		offset += 4
	}

	// Write footer (80 bytes).
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

	// Flush and close the file.
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}
	// Sync the file to durable storage before closing. Without Sync, data may
	// remain in OS page cache; a subsequent mmap (in Reader.Open) may observe a
	// stale view of the file and fail to see the tail blocks. This is the
	// cause of "key not found after flush" for the last blocks.
	// See: PROMPT-SSTABLE-03, MMAP-STALE-01
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync SSTable file: %w", err)
	}
	if err := w.file.Close(); err != nil {
		return fmt.Errorf("failed to close SSTable file: %w", err)
	}
	return nil
}

// encodeEntry serializes a key and value into a binary entry.
// Format: [keyLen:4][key:keyLen][valLen:4][val:valLen].
// The value is stored with a leading type tag (v0.4+) to distinguish
// inline values, value pointers, and tombstones. The value passed here is
// expected to be in the UNtagged form; callers that already hold a tagged
// value must use encodeEntryTagged (via Writer.AppendTagged).
func encodeEntry(key, value []byte) []byte {
	kl := len(key)
	// Value storage: 1 tag byte + payload (nil → 1-byte tombstone).
	var storedLen int
	if value == nil {
		storedLen = 1
	} else {
		storedLen = 1 + len(value)
	}
	buf := make([]byte, 4+4+kl+storedLen)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(kl))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(storedLen))
	copy(buf[8:8+kl], key)
	valPos := 8 + kl
	if value == nil {
		buf[valPos] = tagTombstone
	} else {
		buf[valPos] = tagInline
		copy(buf[valPos+1:], value)
	}
	return buf
}

// encodeEntryTagged serializes a key and an already-tagged value into a binary
// entry. The value is stored verbatim (tag + payload) without re-tagging, so
// the reader can resolve a TypeValuePointer through the VLog. Format:
// [keyLen:4][key:keyLen][valLen:4][val:valLen]. See: WIS-KEY-01.
func encodeEntryTagged(key, value []byte) []byte {
	kl := len(key)
	var storedLen int
	if value == nil {
		storedLen = 1
	} else {
		storedLen = len(value)
	}
	buf := make([]byte, 4+4+kl+storedLen)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(kl))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(storedLen))
	copy(buf[8:8+kl], key)
	valPos := 8 + kl
	if value == nil {
		buf[valPos] = tagTombstone
	} else {
		copy(buf[valPos:], value)
	}
	return buf
}

// Footer is the fixed-size structure at the end of every SSTable file.
// It contains offsets and sizes of the index, Bloom filter, and min/max keys.
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
