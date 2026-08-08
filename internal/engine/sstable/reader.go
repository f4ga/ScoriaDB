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
	"hash/crc32"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// MmapFile is a platform-independent interface for memory-mapped file access.
//
// Contract:
//   - Data() returns a byte slice backed by the mmap region (or nil on fallback).
//     The slice remains valid until Close() is called.
//   - Close() releases all resources (munmap or close).
//   - ReadAt() is available as a fallback for platforms without mmap.
//
// Implementations:
//   - Linux, macOS: syscall.Mmap with PROT_READ and MAP_SHARED
//   - Windows: CreateFileMapping + MapViewOfFile
//   - Other platforms: os.File (fallback, Data() returns nil)
//
// See: ARCH-MMAP-01
type MmapFile interface {
	Data() []byte                            // entire file as a slice (nil if mmap unavailable)
	Size() int64                             // file size
	ReadAt(p []byte, off int64) (int, error) // fallback for non-mmap platforms
	Close() error
}

// Reader reads SSTable from a memory-mapped file.
//
// Before this change, each SSTable Lookup performed three system calls:
// open(), lseek(), and read(). With millions of Lookups per second, this
// created significant CPU overhead and GC pressure.
//
// After this change, Lookup accesses the mmap region directly through memory
// accesses — zero system calls, zero allocations. Block data is returned as
// a slice referencing the mmap region, not a copied buffer.
//
// See: ARCH-MMAP-06, PERF-MMAP-03
type Reader struct {
	mmapFile     MmapFile
	footer       Footer
	indexEntries []IndexEntry
	bloomFilter  *BloomFilter
	minKey       []byte
	maxKey       []byte
	path         string
	closed       atomic.Bool
}

// IndexEntry represents a block index entry.
// The key is the raw user key (without timestamp) of the first key in the block.
// Binary search compares user keys directly. Block ordering is guaranteed by
// Writer.Finish() which sorts blocks before writing.
// See: PROMPT-SSTABLE-FINAL
type IndexEntry struct {
	key    []byte // raw user key (first key in block, without timestamp)
	offset uint64
}

// Open opens an SSTable file for reading using memory-mapped I/O.
//
// The file is memory-mapped immediately. On platforms that support mmap
// (Linux, macOS, Windows), this provides zero-copy access to the file data.
// On other platforms, it falls back to traditional file I/O.
//
// The footer, index, Bloom filter, and range keys are parsed eagerly from
// the mmap region. Data blocks are read lazily on Lookup/iteration.
//
// Returns an error if:
//   - The file cannot be opened
//   - The file is empty
//   - The footer is corrupted (invalid magic number)
//   - The index or Bloom filter data is corrupted
//
// See: ARCH-MMAP-07
func Open(path string) (*Reader, error) {
	mmapFile, err := openMmapFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SSTable file: %w", err)
	}

	data := mmapFile.Data()
	if data == nil {
		// Fallback platform: use file I/O via ReadAt
		return openFallback(mmapFile, path)
	}

	// Fast path: parse from mmap region (0 allocations for data access)
	fileSize := len(data)

	// Read footer from the end of the file
	// Footer is 80 bytes: 8 fields × 8 bytes each
	const footerSize = 80
	if fileSize < footerSize {
		mmapFile.Close()
		return nil, fmt.Errorf("file too small for footer: %d bytes", fileSize)
	}

	footerData := data[fileSize-footerSize:]
	var footer Footer
	footer.IndexOffset = binary.LittleEndian.Uint64(footerData[0:8])
	footer.IndexSize = binary.LittleEndian.Uint64(footerData[8:16])
	footer.BloomOffset = binary.LittleEndian.Uint64(footerData[16:24])
	footer.BloomSize = binary.LittleEndian.Uint64(footerData[24:32])
	footer.NumKeys = binary.LittleEndian.Uint64(footerData[32:40])
	footer.Magic = binary.LittleEndian.Uint64(footerData[40:48])
	footer.MinKeyOffset = binary.LittleEndian.Uint64(footerData[48:56])
	footer.MinKeyLength = binary.LittleEndian.Uint64(footerData[56:64])
	footer.MaxKeyOffset = binary.LittleEndian.Uint64(footerData[64:72])
	footer.MaxKeyLength = binary.LittleEndian.Uint64(footerData[72:80])

	if footer.Magic != MagicNumber {
		mmapFile.Close()
		return nil, fmt.Errorf("invalid SSTable magic number: 0x%x", footer.Magic)
	}

	// Parse index from mmap region
	indexEntries, err := parseIndex(data, footer.IndexOffset, footer.IndexSize)
	if err != nil {
		mmapFile.Close()
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}

	// Parse Bloom filter from mmap region
	bloomFilter, err := parseBloomFilter(data, footer.BloomOffset, footer.BloomSize)
	if err != nil {
		mmapFile.Close()
		return nil, fmt.Errorf("failed to parse bloom filter: %w", err)
	}

	// Parse min/max keys from mmap region
	minKey := parseKey(data, footer.MinKeyOffset, footer.MinKeyLength)
	maxKey := parseKey(data, footer.MaxKeyOffset, footer.MaxKeyLength)

	return &Reader{
		mmapFile:     mmapFile,
		footer:       footer,
		indexEntries: indexEntries,
		bloomFilter:  bloomFilter,
		minKey:       minKey,
		maxKey:       maxKey,
		path:         path,
	}, nil
}

// openFallback opens an SSTable on platforms without mmap support.
// Uses the MmapFile's ReadAt() method (backed by os.File).
func openFallback(mmapFile MmapFile, path string) (*Reader, error) {
	fileSize := mmapFile.Size()

	// Read footer
	const footerSize = 80
	if fileSize < footerSize {
		mmapFile.Close()
		return nil, fmt.Errorf("file too small for footer: %d bytes", fileSize)
	}

	footerBuf := make([]byte, footerSize)
	if _, err := mmapFile.ReadAt(footerBuf, fileSize-footerSize); err != nil {
		mmapFile.Close()
		return nil, fmt.Errorf("failed to read footer: %w", err)
	}

	var footer Footer
	footer.IndexOffset = binary.LittleEndian.Uint64(footerBuf[0:8])
	footer.IndexSize = binary.LittleEndian.Uint64(footerBuf[8:16])
	footer.BloomOffset = binary.LittleEndian.Uint64(footerBuf[16:24])
	footer.BloomSize = binary.LittleEndian.Uint64(footerBuf[24:32])
	footer.NumKeys = binary.LittleEndian.Uint64(footerBuf[32:40])
	footer.Magic = binary.LittleEndian.Uint64(footerBuf[40:48])
	footer.MinKeyOffset = binary.LittleEndian.Uint64(footerBuf[48:56])
	footer.MinKeyLength = binary.LittleEndian.Uint64(footerBuf[56:64])
	footer.MaxKeyOffset = binary.LittleEndian.Uint64(footerBuf[64:72])
	footer.MaxKeyLength = binary.LittleEndian.Uint64(footerBuf[72:80])

	if footer.Magic != MagicNumber {
		mmapFile.Close()
		return nil, fmt.Errorf("invalid SSTable magic number: 0x%x", footer.Magic)
	}

	// Read index
	indexBuf := make([]byte, footer.IndexSize)
	if _, err := mmapFile.ReadAt(indexBuf, int64(footer.IndexOffset)); err != nil {
		mmapFile.Close()
		return nil, fmt.Errorf("failed to read index: %w", err)
	}
	indexEntries := parseIndexBytes(indexBuf)

	// Read Bloom filter
	bloomBuf := make([]byte, footer.BloomSize)
	if _, err := mmapFile.ReadAt(bloomBuf, int64(footer.BloomOffset)); err != nil {
		mmapFile.Close()
		return nil, fmt.Errorf("failed to read bloom filter: %w", err)
	}
	// First 4 bytes are bloom size, rest is bloom data
	var bloomData []byte
	if len(bloomBuf) >= 4 {
		bloomSize := binary.LittleEndian.Uint32(bloomBuf[0:4])
		if int(bloomSize) <= len(bloomBuf)-4 {
			bloomData = bloomBuf[4 : 4+bloomSize]
		}
	}
	bloomFilter := DecodeBloomFilter(bloomData)

	// Read min/max keys
	var minKey []byte
	if footer.MinKeyLength > 0 {
		minKeyBuf := make([]byte, 4+footer.MinKeyLength)
		if _, err := mmapFile.ReadAt(minKeyBuf, int64(footer.MinKeyOffset)); err == nil {
			keyLen := binary.LittleEndian.Uint32(minKeyBuf[0:4])
			if int(keyLen) == int(footer.MinKeyLength) {
				minKey = make([]byte, keyLen)
				copy(minKey, minKeyBuf[4:4+keyLen])
			}
		}
	}

	var maxKey []byte
	if footer.MaxKeyLength > 0 {
		maxKeyBuf := make([]byte, 4+footer.MaxKeyLength)
		if _, err := mmapFile.ReadAt(maxKeyBuf, int64(footer.MaxKeyOffset)); err == nil {
			keyLen := binary.LittleEndian.Uint32(maxKeyBuf[0:4])
			if int(keyLen) == int(footer.MaxKeyLength) {
				maxKey = make([]byte, keyLen)
				copy(maxKey, maxKeyBuf[4:4+keyLen])
			}
		}
	}

	return &Reader{
		mmapFile:     mmapFile,
		footer:       footer,
		indexEntries: indexEntries,
		bloomFilter:  bloomFilter,
		minKey:       minKey,
		maxKey:       maxKey,
		path:         path,
	}, nil
}

// parseIndex parses the block index from the mmap region.
// The index stores raw user keys (without timestamp) for binary search.
// Returns a slice of IndexEntry with copied keys.
//
// Index keys are copied (not mmap-backed) because they are accessed during
// binary search in Lookup() BEFORE any bounds check. If the file is truncated
// externally, mmap-backed index keys would cause SIGBUS.
//
// Index keys are small metadata (typically < 100 bytes), so the copy overhead
// is negligible compared to the safety gain.
//
// See: SAFE-MMAP-03, ARCH-SSTABLE-IDX-01, PROMPT-SSTABLE-FINAL
func parseIndex(data []byte, offset, size uint64) ([]IndexEntry, error) {
	if offset+size > uint64(len(data)) {
		return nil, fmt.Errorf("index out of range: offset=%d, size=%d, file=%d", offset, size, len(data))
	}

	indexData := data[offset : offset+size]
	entries := make([]IndexEntry, 0, 16) // small initial cap, grows as needed

	pos := uint64(0)
	for pos < size {
		if pos+4 > size {
			return nil, fmt.Errorf("truncated index: cannot read key length at pos %d", pos)
		}
		keyLen := binary.LittleEndian.Uint32(indexData[pos:])
		pos += 4

		if pos+uint64(keyLen)+8 > size {
			return nil, fmt.Errorf("truncated index: key length %d exceeds remaining %d", keyLen, size-pos)
		}

		// Copy the key — index keys are accessed during binary search in Lookup()
		// before any bounds check. mmap-backed slices would cause SIGBUS if the
		// file is truncated externally. Keys are small metadata, so copy is safe.
		key := make([]byte, keyLen)
		copy(key, indexData[pos:pos+uint64(keyLen)])
		pos += uint64(keyLen)

		entryOffset := binary.LittleEndian.Uint64(indexData[pos:])
		pos += 8

		entries = append(entries, IndexEntry{key: key, offset: entryOffset})
	}

	return entries, nil
}

// parseIndexBytes parses the block index from a copied buffer (fallback path).
func parseIndexBytes(data []byte) []IndexEntry {
	entries := make([]IndexEntry, 0, 16)
	pos := 0
	for pos+4 <= len(data) {
		keyLen := binary.LittleEndian.Uint32(data[pos:])
		pos += 4

		if pos+int(keyLen)+8 > len(data) {
			break
		}

		key := make([]byte, keyLen)
		copy(key, data[pos:pos+int(keyLen)])
		pos += int(keyLen)

		offset := binary.LittleEndian.Uint64(data[pos:])
		pos += 8

		entries = append(entries, IndexEntry{key: key, offset: offset})
	}
	return entries
}

// parseBloomFilter parses the Bloom filter from the mmap region.
func parseBloomFilter(data []byte, offset, size uint64) (*BloomFilter, error) {
	if offset+size > uint64(len(data)) {
		return nil, fmt.Errorf("bloom filter out of range: offset=%d, size=%d, file=%d", offset, size, len(data))
	}

	bloomSection := data[offset : offset+size]
	if len(bloomSection) < 4 {
		return DecodeBloomFilter(nil), nil
	}

	bloomSize := binary.LittleEndian.Uint32(bloomSection[0:4])
	if int(bloomSize) > len(bloomSection)-4 {
		return DecodeBloomFilter(nil), nil
	}

	bloomData := bloomSection[4 : 4+bloomSize]
	return DecodeBloomFilter(bloomData), nil
}

// parseKey parses a key from the mmap region.
func parseKey(data []byte, offset, length uint64) []byte {
	if length == 0 || offset+4+length > uint64(len(data)) {
		return nil
	}

	keySection := data[offset : offset+4+length]
	keyLen := binary.LittleEndian.Uint32(keySection[0:4])
	if int(keyLen) != int(length) {
		return nil
	}

	// Copy the key — it's small and used for range checks
	key := make([]byte, keyLen)
	copy(key, keySection[4:4+keyLen])
	return key
}

// blockPool is a sync.Pool for reusing block buffers during SSTable reads
// on fallback platforms. On mmap platforms, this pool is not used.
// Stores *[]byte to satisfy staticcheck (sync.Pool with pointer types).
var blockPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 4096)
		return &buf
	},
}

// readBlock reads a data block from the SSTable at the given offset.
//
// Fast path (mmap): returns a slice of the mmap region — 0 allocations, 0 syscalls.
// Slow path (fallback): reads into a pooled buffer via ReadAt.
//
// The returned buffer includes the CRC32 checksum at the end (last 4 bytes).
// Callers must verify the CRC32 via VerifyBlockCRC or use the block data
// which excludes the CRC32 trailer.
//
// See: PERF-MMAP-04
func (r *Reader) readBlock(offset uint64) ([]byte, error) {
	data := r.mmapFile.Data()
	if data != nil {
		// Fast path: slice from mmap region
		// 0 allocations, 0 syscalls
		return r.readBlockFromMmap(data, offset)
	}

	// Slow path: fallback to file I/O
	return r.readBlockFromFile(offset)
}

// readBlockFromMmap reads a block directly from the mmap region.
// Zero allocations — returns a slice of the mmap region.
// Zero syscalls — pure memory access.
//
// Bounds checking protects against SIGBUS if the file was truncated externally.
//
// See: PERF-MMAP-05, SAFE-MMAP-02
func (r *Reader) readBlockFromMmap(data []byte, offset uint64) ([]byte, error) {
	// Check bounds: need at least 4 bytes for block size
	if int(offset+4) > len(data) {
		return nil, fmt.Errorf("block offset %d out of range (file size %d)", offset, len(data))
	}

	// Read block size from mmap
	blockSize := binary.LittleEndian.Uint32(data[offset:])

	// Block on disk includes CRC32 trailer (4 bytes), so total = blockSize + 4
	totalSize := uint64(blockSize) + 4

	// Check bounds for the entire block
	if int(offset+4+totalSize) > len(data) {
		return nil, fmt.Errorf("block at offset %d (size %d) out of range (file size %d)",
			offset, totalSize, len(data))
	}

	// Slice the block data from mmap — 0 allocs
	blockData := data[offset : offset+4+totalSize]

	// Verify CRC32 before returning
	if err := verifyBlockCRC(blockData[4:]); err != nil {
		return nil, err
	}

	// Return block data without the size prefix and without CRC32 trailer
	return blockData[4 : 4+blockSize], nil
}

// readBlockFromFile reads a block using file I/O (fallback for non-mmap platforms).
func (r *Reader) readBlockFromFile(offset uint64) ([]byte, error) {
	var blockSize uint32
	sizeBuf := make([]byte, 4)
	if _, err := r.mmapFile.ReadAt(sizeBuf, int64(offset)); err != nil {
		return nil, fmt.Errorf("failed to read block size: %w", err)
	}
	blockSize = binary.LittleEndian.Uint32(sizeBuf)

	// Block on disk includes CRC32 trailer (4 bytes), so total = blockSize + 4
	totalSize := blockSize + 4

	bufPtr, ok := blockPool.Get().(*[]byte)
	if !ok {
		buf := make([]byte, totalSize)
		if _, err := r.mmapFile.ReadAt(buf, int64(offset+4)); err != nil {
			return nil, fmt.Errorf("failed to read block data: %w", err)
		}
		// Verify CRC32 before returning
		if err := verifyBlockCRC(buf); err != nil {
			return nil, err
		}
		return buf[:blockSize], nil
	}
	buf := *bufPtr
	if cap(buf) < int(totalSize) {
		buf = make([]byte, totalSize)
	} else {
		buf = buf[:totalSize]
	}

	if _, err := r.mmapFile.ReadAt(buf, int64(offset+4)); err != nil {
		blockPool.Put(&buf)
		return nil, fmt.Errorf("failed to read block data: %w", err)
	}

	// Verify CRC32 before returning
	if err := verifyBlockCRC(buf); err != nil {
		blockPool.Put(&buf)
		return nil, err
	}

	return buf[:blockSize], nil
}

// verifyBlockCRC checks the CRC32 checksum stored in the last 4 bytes of buf
// against the data in buf[:len(buf)-4].
func verifyBlockCRC(buf []byte) error {
	if len(buf) < 4 {
		return fmt.Errorf("block too short for CRC: %d bytes", len(buf))
	}
	data := buf[:len(buf)-4]
	storedCRC := binary.LittleEndian.Uint32(buf[len(buf)-4:])
	computedCRC := crc32.ChecksumIEEE(data)
	if storedCRC != computedCRC {
		return fmt.Errorf("block CRC mismatch: stored 0x%08x, computed 0x%08x", storedCRC, computedCRC)
	}
	return nil
}

// ReleaseBlock returns a buffer to the pool.
// Only needed on fallback platforms — on mmap platforms, blocks are not pooled.
func ReleaseBlock(buf []byte) {
	blockPool.Put(&buf)
}

// Lookup searches for a key in the SSTable and returns the value if found.
//
// Fast path (mmap): 0 syscalls, 0 allocations.
//   - Range filter: compare key against min/max (no I/O)
//   - Bloom filter: check key existence (no I/O)
//   - Binary search on index (no I/O — index is in mmap)
//   - Read block from mmap (0 allocs, 0 syscalls)
//   - Linear scan within block (0 allocs)
//
// Slow path (fallback): uses ReadAt for block reads.
//
// The returned value slice points directly to the mmap region.
// The value is valid only until the Reader is closed.
// The caller MUST copy the value if it needs to persist beyond the lifetime
// of the Reader.
//
// This is a zero-allocation optimization. The returned slice is a reference
// to the mmap region, not a copy. Use with caution.
//
// See: PERF-MMAP-06
func (r *Reader) Lookup(key mvcc.MVCCKey) ([]byte, bool) {
	if r.closed.Load() {
		return nil, false
	}

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

	// Find block using binary search on index.
	// The index stores raw user keys (first key of each block, without timestamp).
	// We compare the lookup user key directly against index keys using bytes.Compare.
	//
	// sort.Search finds the first index where indexKey > userKey.
	// The target block is the one before that (where indexKey <= userKey).
	blockIdx := sort.Search(len(r.indexEntries), func(i int) bool {
		return bytes.Compare(r.indexEntries[i].key, userKey) > 0
	})
	blockIdx-- // previous block is the one where indexKey <= userKey
	if blockIdx < 0 {
		return nil, false // key is before the first block
	}

	// Read the block
	blockOffset := r.indexEntries[blockIdx].offset
	blockData, err := r.readBlock(blockOffset)
	if err != nil {
		return nil, false
	}

	// On mmap platforms, blockData is a slice of the mmap region — no release needed.
	// On fallback platforms, blockData is from the pool and must be released.
	// We detect mmap vs fallback by checking if Data() returns non-nil.
	isMmap := r.mmapFile.Data() != nil
	if !isMmap {
		defer ReleaseBlock(blockData)
	}

	// Search for the key in the block.
	//
	// Within a block, versions of the same user key are stored oldest-first
	// (ascending commitTS). For a snapshot we must return the NEWEST version
	// with commitTS <= snapshotTS, so we scan the whole group and keep the
	// last (newest) qualifying entry. If that newest visible version is a
	// tombstone, the key is deleted for this snapshot.
	snapshotTS := key.CommitTS()

	var bestVal []byte
	bestFound := false
	bestTombstone := false

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
			commitTS := mvccKey.CommitTS()
			if commitTS > snapshotTS {
				// Version committed after the snapshot — not visible. Continue
				// scanning for older versions of the same key.
				continue
			}
			// A tombstone is either an empty value (legacy) or a single
			// TypeTombstone tag byte (v0.4+).
			isTomb := len(entryVal) == 0 || (len(entryVal) == 1 && entryVal[0] == tagTombstone)
			val := entryVal
			if !isTomb && isValidValueTag(entryVal[0]) {
				// Strip the leading type tag (v0.4+). Legacy values have no tag
				// and are returned as-is. The tag is not part of the user value.
				val = entryVal[1:]
			}
			// Later entries in the group are newer; keep the newest visible.
			bestVal = val
			bestFound = true
			bestTombstone = isTomb
		} else if cmp > 0 {
			// Since the block is sorted, if we've passed the key, it's not here
			break
		}
	}

	if !bestFound || bestTombstone {
		return nil, false
	}
	// Zero-allocation return: bestVal is a slice of the mmap region.
	// The value is valid only until the Reader is closed.
	// Callers must copy if they need the value beyond the Reader lifetime.
	return bestVal, true
}

// Close closes the SSTable reader and releases the mmap region.
//
// Safe to call multiple times (idempotent after first call).
// On mmap platforms, this calls munmap.
// On fallback platforms, this closes the underlying file.
//
// See: ARCH-MMAP-04
func (r *Reader) Close() error {
	if r.closed.Load() {
		return nil
	}
	r.closed.Store(true)

	if r.mmapFile != nil {
		return r.mmapFile.Close()
	}
	return nil
}

// Path returns the file path of the SSTable.
func (r *Reader) Path() string {
	return r.path
}

// NumKeys returns the number of keys in the SSTable.
func (r *Reader) NumKeys() uint64 {
	return r.footer.NumKeys
}

// Footer returns the SSTable footer.
func (r *Reader) Footer() Footer {
	return r.footer
}

// IndexEntries returns the block index entries.
func (r *Reader) IndexEntries() []IndexEntry {
	return r.indexEntries
}

// BloomFilter returns the Bloom filter.
func (r *Reader) BloomFilter() *BloomFilter {
	return r.bloomFilter
}

// MinKey returns the minimum key in the SSTable.
func (r *Reader) MinKey() []byte {
	return r.minKey
}

// MaxKey returns the maximum key in the SSTable.
func (r *Reader) MaxKey() []byte {
	return r.maxKey
}
