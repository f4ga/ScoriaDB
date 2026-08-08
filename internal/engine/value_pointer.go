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
	"unsafe"
)

// ValuePointerSize is the encoded size of a ValuePointer in bytes.
// Computed as unsafe.Sizeof(int64(0)) + unsafe.Sizeof(int32(0)) = 8 + 4 = 12 bytes.
// Using unsafe.Sizeof makes the relationship to the struct fields explicit:
//   - Offset int64 → unsafe.Sizeof(int64(0)) bytes
//   - Size   int32 → unsafe.Sizeof(int32(0)) bytes
const ValuePointerSize = int(unsafe.Sizeof(int64(0)) + unsafe.Sizeof(int32(0))) // 12 bytes

// Type tags distinguish how a stored value is encoded. The tag is a single
// leading byte prepended to every value written in the NEW format (v0.4+).
// It resolves the DEF-02 / DEF-04 ambiguity where a user value of exactly
// ValuePointerSize (12) bytes was indistinguishable from a ValuePointer.
//
// Backward compatibility: a stored value whose first byte is NOT a valid tag
// (0x00, 0x01, 0x02) is treated as the OLD format without a tag, using the
// length heuristic (len == ValuePointerSize) to detect a ValuePointer.
const (
	// TypeInline is a regular (small) value stored inline.
	TypeInline = 0x00
	// TypeValuePointer is a 12-byte ValuePointer into VLog/unified-mmap.
	TypeValuePointer = 0x01
	// TypeTombstone is an empty (deleted) value.
	TypeTombstone = 0x02
)

// IsValidValueTag reports whether the first byte of a stored value is a
// well-known type tag. Values in the old format (no tag) return false, so
// callers fall back to the length heuristic.
func IsValidValueTag(b byte) bool {
	return b == TypeInline || b == TypeValuePointer || b == TypeTombstone
}

// TaggedStorageSize returns the total bytes needed to store value in the
// tagged format: 1 tag byte + len(value). The tombstone tag uses no payload.
// If value is nil, a single TypeTombstone byte is returned.
func TaggedStorageSize(value []byte) int {
	if value == nil {
		return 1
	}
	return 1 + len(value)
}

// EncodeTaggedValue writes the tagged form of value into dst (must be at least
// TaggedStorageSize(value) bytes). Returns the number of bytes written.
// Zero allocations.
func EncodeTaggedValue(value []byte, dst []byte) int {
	if value == nil {
		dst[0] = TypeTombstone
		return 1
	}
	dst[0] = TypeInline
	n := copy(dst[1:], value)
	return 1 + n
}

// IsStoredValuePointer reports whether a stored value is a ValuePointer.
// It respects the leading type tag (new format); values without a valid tag
// fall back to the legacy length heuristic. See DEF-02 / DEF-04.
func IsStoredValuePointer(stored []byte) bool {
	if len(stored) == 0 {
		return false
	}
	if IsValidValueTag(stored[0]) {
		return stored[0] == TypeValuePointer
	}
	// Legacy format: no tag — infer from length.
	return len(stored) == ValuePointerSize
}

// ValuePointer points to a value in the Value Log.
type ValuePointer struct {
	Offset int64
	Size   int32
}

// EncodeValuePointer encodes a ValuePointer into buf (must be at least ValuePointerSize bytes).
// Returns the number of bytes written. Zero allocations.
// buf length is NOT checked in hot path — caller must ensure capacity.
//
//go:inline
func EncodeValuePointer(vp ValuePointer, buf []byte) int {
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:ValuePointerSize], uint32(vp.Size))
	return ValuePointerSize
}

// DecodeValuePointer decodes ValuePointerSize bytes into a ValuePointer.
func DecodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != ValuePointerSize {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:ValuePointerSize])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}
