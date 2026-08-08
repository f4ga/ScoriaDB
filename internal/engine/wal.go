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
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"

	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

// ============================================================
// WalEntry Pool — zero allocation in hot path
// ============================================================

var walEntryPool = sync.Pool{
	New: func() interface{} { return &WalEntry{} },
}

func newWalEntry() *WalEntry {
	entry, ok := walEntryPool.Get().(*WalEntry)
	if !ok {
		return &WalEntry{}
	}
	return entry
}

func putWalEntry(entry *WalEntry) {
	entry.Key = nil
	entry.Value = nil
	entry.Timestamp = 0
	entry.Op = 0
	entry.IsLarge = false
	walEntryPool.Put(entry)
}

// ============================================================
// WAL Types
// ============================================================

type OpType byte

const (
	OpPut    OpType = 1
	OpDelete OpType = 2
	OpBatch  OpType = 3
)

// WalEntry represents a WAL entry.
// IsLarge flag indicates that Value is a ValuePointer (12 bytes),
// not the actual data. Used for large values (>MaxInlineSize)
// to reduce Write Amplification from 2x to 1x.
type WalEntry struct {
	Op        OpType
	Key       []byte
	Value     []byte
	Timestamp uint64
	IsLarge   bool // if true, Value is 12-byte ValuePointer
}

// ============================================================
// WAL Core
// ============================================================

const maxWalEntrySize = 1 * 1024 * 1024 // 1MB

type WAL struct {
	mu        sync.Mutex
	file      *os.File
	offset    int64
	opts      WALOptions
	group     *groupCommitWriter
	encodeBuf []byte // pre-allocated, zero allocations in hot path
}

func OpenWAL(path string) (*WAL, error) {
	return OpenWALWithOptions(path, DefaultWALOptions())
}

func OpenWALWithOptions(path string, opts WALOptions) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open wal file: %w", err)
	}

	stat, err := file.Stat()
	if err != nil {
		errors.CloseWithLog(file, "wal-file")
		return nil, fmt.Errorf("failed to stat wal file: %w", err)
	}

	wal := &WAL{
		file:      file,
		offset:    stat.Size(),
		opts:      opts,
		encodeBuf: make([]byte, maxWalEntrySize),
	}

	if opts.GroupCommitEnabled {
		wal.group = newGroupCommitWriter(file, opts.GroupCommitInterval, opts.SyncMode)
	}

	return wal, nil
}

// Write encodes entry directly into pre-allocated buffer.
// Zero allocations in hot path.
func (w *WAL) Write(entry *WalEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	n, err := encodeWalEntryTo(entry, w.encodeBuf)
	if err != nil {
		return fmt.Errorf("failed to encode wal entry: %w", err)
	}

	buf := w.encodeBuf[:n]

	if w.group != nil {
		if err := w.group.Write(buf); err != nil {
			return fmt.Errorf("failed to write to group commit buffer: %w", err)
		}
		w.offset += int64(n)
		return nil
	}

	if _, err := w.file.Write(buf); err != nil {
		return fmt.Errorf("failed to write wal entry: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync wal: %w", err)
	}
	w.offset += int64(n)
	return nil
}

func (w *WAL) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.group != nil {
		return w.group.Flush()
	}
	return nil
}

func (w *WAL) Recover(cb func(*WalEntry) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek wal: %w", err)
	}

	reader := io.Reader(w.file)
	for {
		entry, err := decodeWalEntry(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			logger.Warn("wal: corrupted entry during recovery, stopping: %v", err)
			break
		}
		if err := cb(entry); err != nil {
			return fmt.Errorf("callback error: %w", err)
		}
	}
	return nil
}

func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.group != nil {
		if err := w.group.Close(); err != nil {
			errors.CloseWithLog(w.file, "wal-file")
			return fmt.Errorf("failed to close group commit writer: %w", err)
		}
	}

	return w.file.Close()
}

// ============================================================
// Encoding — zero allocation in hot path
// ============================================================

// WAL record format (v0.4+):
//
//	[0:1]    Op (1 byte)
//	[1:2]    Flags (1 byte) — bit 0 = IsLarge
//	[2:10]   Timestamp (8 bytes)
//	[10:12]  KeyLen (2 bytes)
//	[12:16]  ValueLen (4 bytes)
//	[16:16+KL] Key (variable)
//	[16+KL:16+KL+VL] Value (variable)
//	[end-4:end] CRC32 (4 bytes)
//
// The IsLarge flag disambiguates a user value of exactly ValuePointerSize (12)
// bytes from a real ValuePointer. See DEF-02 / DEF-04.
const walFlagIsLarge byte = 0x01

// encodeWalEntryTo writes entry directly into dst buffer.
// Returns number of bytes written. Zero allocations.
func encodeWalEntryTo(entry *WalEntry, dst []byte) (int, error) {
	keyLen := len(entry.Key)
	valLen := len(entry.Value)
	// Header grew from 15 to 16 bytes (1 op + 1 flags + 8 ts + 2 klen + 4 vlen).
	totalSize := 1 + 1 + 8 + 2 + 4 + keyLen + valLen + 4 // + CRC
	if totalSize > len(dst) {
		return 0, fmt.Errorf("encodeWalEntryTo: dst too small: need %d, have %d", totalSize, len(dst))
	}

	pos := 0
	dst[pos] = byte(entry.Op)
	pos++

	var flags byte
	if entry.IsLarge {
		flags |= walFlagIsLarge
	}
	dst[pos] = flags
	pos++

	binary.BigEndian.PutUint64(dst[pos:pos+8], entry.Timestamp)
	pos += 8

	binary.BigEndian.PutUint16(dst[pos:pos+2], uint16(keyLen))
	pos += 2

	binary.BigEndian.PutUint32(dst[pos:pos+4], uint32(valLen))
	pos += 4

	copy(dst[pos:pos+keyLen], entry.Key)
	pos += keyLen

	copy(dst[pos:pos+valLen], entry.Value)
	pos += valLen

	crc := crc32.ChecksumIEEE(dst[:pos])
	binary.BigEndian.PutUint32(dst[pos:pos+4], crc)
	pos += 4

	return pos, nil
}

// decodeWalEntry reads an entry from the stream.
// NOTE: This allocates key/value slices during recovery only,
// not in hot path.
func decodeWalEntry(r io.Reader) (*WalEntry, error) {
	header := make([]byte, 1+1+8+2+4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	op := OpType(header[0])
	flags := header[1]
	timestamp := binary.BigEndian.Uint64(header[2:10])
	keyLen := binary.BigEndian.Uint16(header[10:12])
	valLen := binary.BigEndian.Uint32(header[12:16])

	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	value := make([]byte, valLen)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, err
	}

	crcBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, crcBuf); err != nil {
		return nil, err
	}
	crcStored := binary.BigEndian.Uint32(crcBuf)

	data := make([]byte, 1+1+8+2+4+int(keyLen)+int(valLen))
	copy(data[0:], header)
	copy(data[1+1+8+2+4:], key)
	copy(data[1+1+8+2+4+int(keyLen):], value)

	if crc := crc32.ChecksumIEEE(data); crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	entry := newWalEntry()
	entry.Op = op
	entry.Key = key
	entry.Value = value
	entry.Timestamp = timestamp

	// Use the on-disk IsLarge flag (bit 0) to disambiguate a real ValuePointer
	// from a user value of exactly ValuePointerSize bytes. This flag is always
	// written correctly for new data. See DEF-02 / DEF-04.
	entry.IsLarge = flags&walFlagIsLarge != 0

	return entry, nil
}
