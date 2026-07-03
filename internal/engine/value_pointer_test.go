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
	"testing"
)

func TestEncodeDecodeValuePointer(t *testing.T) {
	original := ValuePointer{Offset: 12345, Size: 6789}
	var buf [12]byte
	encodeValuePointer(original, buf[:])

	decoded, ok := decodeValuePointer(buf[:])
	if !ok {
		t.Fatal("decodeValuePointer failed")
	}

	if decoded.Offset != original.Offset {
		t.Errorf("Offset mismatch: expected %d, got %d", original.Offset, decoded.Offset)
	}
	if decoded.Size != original.Size {
		t.Errorf("Size mismatch: expected %d, got %d", original.Size, decoded.Size)
	}
}

func TestDecodeValuePointerInvalid(t *testing.T) {
	_, ok := decodeValuePointer([]byte{1, 2, 3})
	if ok {
		t.Error("decodeValuePointer should return false for short data")
	}

	_, ok = decodeValuePointer(nil)
	if ok {
		t.Error("decodeValuePointer should return false for nil")
	}
}

func TestValuePointerZero(t *testing.T) {
	vp := ValuePointer{Offset: 0, Size: 0}
	var buf [12]byte
	encodeValuePointer(vp, buf[:])
	decoded, ok := decodeValuePointer(buf[:])
	if !ok {
		t.Fatal("decodeValuePointer failed for zero pointer")
	}
	if decoded.Offset != 0 || decoded.Size != 0 {
		t.Errorf("expected zero pointer, got %+v", decoded)
	}
}
