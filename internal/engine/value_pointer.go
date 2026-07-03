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

// ValuePointer points to a value in the Value Log.
type ValuePointer struct {
	Offset int64
	Size   int32
}

// encodeValuePointer encodes a ValuePointer into buf (must be at least ValuePointerSize bytes).
// Returns the number of bytes written. Zero allocations.
// buf length is NOT checked in hot path — caller must ensure capacity.
//
//go:inline
func encodeValuePointer(vp ValuePointer, buf []byte) int {
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:ValuePointerSize], uint32(vp.Size))
	return ValuePointerSize
}

// decodeValuePointer decodes ValuePointerSize bytes into a ValuePointer.
func decodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != ValuePointerSize {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:ValuePointerSize])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}
