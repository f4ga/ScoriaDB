package engine

import (
	"testing"
)

func TestEncodeDecodeValuePointer(t *testing.T) {
	original := ValuePointer{Offset: 12345, Size: 6789}
	encoded := encodeValuePointer(original)

	decoded, ok := decodeValuePointer(encoded)
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
	// Слишком короткие данные
	_, ok := decodeValuePointer([]byte{1, 2, 3})
	if ok {
		t.Error("decodeValuePointer should return false for short data")
	}

	// Пустые данные
	_, ok = decodeValuePointer(nil)
	if ok {
		t.Error("decodeValuePointer should return false for nil")
	}
}

func TestValuePointerZero(t *testing.T) {
	vp := ValuePointer{Offset: 0, Size: 0}
	encoded := encodeValuePointer(vp)
	decoded, ok := decodeValuePointer(encoded)
	if !ok {
		t.Fatal("decodeValuePointer failed for zero pointer")
	}
	if decoded.Offset != 0 || decoded.Size != 0 {
		t.Errorf("expected zero pointer, got %+v", decoded)
	}
}
