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
)

// encodeValuePointer encodes a ValuePointer into 12 bytes.
func encodeValuePointer(vp ValuePointer) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(vp.Size))
	return buf
}

// decodeValuePointer decodes 12 bytes into a ValuePointer.
func decodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != 12 {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:12])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}

// ReadVLogValue reads a value from VLog by fileID and offset.
func (e *LSMEngine) ReadVLogValue(fileID uint64, offset uint32) ([]byte, error) {
	if e.vlog == nil {
		return nil, fmt.Errorf("vlog not initialized")
	}
	vp := ValuePointer{Offset: int64(fileID), Size: int32(offset)}
	return e.vlog.Read(vp)
}

// decodeStoredValue decodes a stored value (inline or VLog pointer).
func (e *LSMEngine) decodeStoredValue(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		return nil, nil
	}
	if vp, ok := decodeValuePointer(stored); ok {
		if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > e.vlog.Size() {
			return stored, nil
		}
		return e.vlog.Read(vp)
	}
	return stored, nil
}
