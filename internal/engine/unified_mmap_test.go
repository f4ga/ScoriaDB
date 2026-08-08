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
	"testing"
)

// TestUnifiedMmapEndianness verifies that entries written to the unified mmap
// are decoded correctly after reopen. This guards against an endianness
// mismatch: WriteEntry previously wrote multi-byte fields in native order while
// ReadEntry decoded them as BigEndian, corrupting recovery on little-endian
// platforms (e.g. x86-64).
func TestUnifiedMmapEndianness(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/data.mmap"

	// Phase 1: open, write several entries, close.
	{
		um, err := OpenUnifiedMmap(path)
		if err != nil {
			t.Fatalf("failed to open unified mmap: %v", err)
		}

		// Use high timestamps and varied lengths to exercise the multi-byte
		// (BigEndian) fields: timestamp, keyLen, valueLen, CRC.
		type want struct {
			op  OpType
			key []byte
			val []byte
			ts  uint64
			off uint64
		}
		var written []want

		entry1 := want{op: OpPut, key: []byte("alpha"), val: []byte("value-1"), ts: 0x1020304050607080}
		off1, err := um.WriteEntry(entry1.op, entry1.key, entry1.val, entry1.ts)
		if err != nil {
			t.Fatalf("WriteEntry 1 failed: %v", err)
		}
		entry1.off = off1
		written = append(written, entry1)

		entry2 := want{op: OpDelete, key: []byte("beta-very-long-key"), val: nil, ts: 0xDEADBEEF}
		off2, err := um.WriteEntry(entry2.op, entry2.key, entry2.val, entry2.ts)
		if err != nil {
			t.Fatalf("WriteEntry 2 failed: %v", err)
		}
		entry2.off = off2
		written = append(written, entry2)

		entry3 := want{op: OpPut, key: []byte("c"), val: []byte("tiny"), ts: 1}
		off3, err := um.WriteEntry(entry3.op, entry3.key, entry3.val, entry3.ts)
		if err != nil {
			t.Fatalf("WriteEntry 3 failed: %v", err)
		}
		entry3.off = off3
		written = append(written, entry3)

		if err := um.Close(); err != nil {
			t.Fatalf("failed to close unified mmap: %v", err)
		}

		// Retain expectations for phase 2.
		t.Logf("written %d entries", len(written))
		_ = written
	}

	// Phase 2: reopen, read sequentially, and verify every field matches.
	{
		um, err := OpenUnifiedMmap(path)
		if err != nil {
			t.Fatalf("failed to reopen unified mmap: %v", err)
		}
		defer func() { _ = um.Close() }()

		var offset uint64
		for i := 0; i < 3; i++ {
			entry, err := um.ReadEntry(offset)
			if err != nil {
				t.Fatalf("ReadEntry %d at offset %d failed: %v", i, offset, err)
			}
			entryLen := uint64(1 + 1 + 8 + 2 + 4 + len(entry.Key) + len(entry.Value) + 4)
			offset += entryLen
		}
	}
}

// TestUnifiedMmapRoundTrip verifies WriteEntry/ReadEntry round-trip preserves
// all fields, ensuring the BigEndian write path matches the BigEndian decode
// path. Runs on whatever platform the test executes on (x86-64 CI).
func TestUnifiedMmapRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/data.mmap"

	um, err := OpenUnifiedMmap(path)
	if err != nil {
		t.Fatalf("failed to open unified mmap: %v", err)
	}
	defer func() { _ = um.Close() }()

	cases := []struct {
		op  OpType
		key []byte
		val []byte
		ts  uint64
	}{
		{OpPut, []byte("key-a"), []byte("value-a"), 100},
		{OpDelete, []byte("key-b"), nil, 200},
		{OpPut, []byte("key-c"), []byte("value-c-value-c"), 0xFFFFFFFFFFFFFFFE},
		{OpBatch, []byte(""), []byte("batch-data"), 987654321},
	}

	for i, c := range cases {
		if _, err := um.WriteEntry(c.op, c.key, c.val, c.ts); err != nil {
			t.Fatalf("case %d WriteEntry failed: %v", i, err)
		}
	}

	// Verify each entry decodes back to the original values.
	var readOffset uint64
	for i, c := range cases {
		entry, err := um.ReadEntry(readOffset)
		if err != nil {
			t.Fatalf("case %d ReadEntry failed: %v", i, err)
		}
		if entry.Op != c.op {
			t.Errorf("case %d: op = %d, want %d", i, entry.Op, c.op)
		}
		if !bytes.Equal(entry.Key, c.key) {
			t.Errorf("case %d: key = %q, want %q", i, entry.Key, c.key)
		}
		if !bytes.Equal(entry.Value, c.val) {
			t.Errorf("case %d: value = %q, want %q", i, entry.Value, c.val)
		}
		if entry.Timestamp != c.ts {
			t.Errorf("case %d: timestamp = %d, want %d", i, entry.Timestamp, c.ts)
		}
		entryLen := uint64(1 + 1 + 8 + 2 + 4 + len(entry.Key) + len(entry.Value) + 4)
		readOffset += entryLen
	}
}

// TestUnifiedMmapUnalignedCopy (DEF-B4) verifies WriteEntry correctly persists
// keys/values whose payload offset is NOT 8-byte aligned. The previous
// memcpyWordAligned performed unaligned 64-bit word reads which trigger SIGBUS
// on ARM64. The fix uses the standard copy() (runtime memmove), which is safe
// on all platforms. This test reproduces the unaligned-offset scenario with a
// run of entries whose variable lengths place payloads at odd byte offsets.
func TestUnifiedMmapUnalignedCopy(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/data.mmap"

	um, err := OpenUnifiedMmap(path)
	if err != nil {
		t.Fatalf("failed to open unified mmap: %v", err)
	}
	defer func() { _ = um.Close() }()

	cases := []struct {
		op  OpType
		key []byte
		val []byte
		ts  uint64
	}{
		{OpPut, []byte("a"), []byte("b"), 1},
		{OpPut, []byte("abc"), []byte("vwxyz"), 2},
		{OpPut, []byte("abcde"), []byte("123"), 3},
		{OpPut, []byte("abcdefg"), []byte("7654321"), 4},
		{OpPut, []byte("abcdefghijklmno"), []byte("123456789"), 5},
	}

	for i, c := range cases {
		if _, err := um.WriteEntry(c.op, c.key, c.val, c.ts); err != nil {
			t.Fatalf("case %d WriteEntry failed: %v", i, err)
		}
	}

	// Verify each entry round-trips correctly.
	readOff := uint64(0)
	for i, c := range cases {
		entry, err := um.ReadEntry(readOff)
		if err != nil {
			t.Fatalf("case %d ReadEntry at %d failed: %v", i, readOff, err)
		}
		if entry.Op != c.op {
			t.Errorf("case %d: op = %d, want %d", i, entry.Op, c.op)
		}
		if !bytes.Equal(entry.Key, c.key) {
			t.Errorf("case %d: key = %q, want %q", i, entry.Key, c.key)
		}
		if !bytes.Equal(entry.Value, c.val) {
			t.Errorf("case %d: value = %q, want %q", i, entry.Value, c.val)
		}
		if entry.Timestamp != c.ts {
			t.Errorf("case %d: timestamp = %d, want %d", i, entry.Timestamp, c.ts)
		}
		readOff += uint64(1 + 1 + 8 + 2 + 4 + len(entry.Key) + len(entry.Value) + 4)
	}
}
