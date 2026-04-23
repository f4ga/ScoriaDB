package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVLogWriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer vlog.Close()

	// Маленькое значение (inline)
	small := []byte("small")
	vp, err := vlog.Write(small)
	if err != nil {
		t.Fatalf("failed to write small value: %v", err)
	}
	if vp.Size != 0 {
		t.Errorf("expected inline value (size 0), got size %d", vp.Size)
	}

	// Большое значение (должно быть записано в VLog)
	large := make([]byte, 100)
	for i := range large {
		large[i] = byte(i)
	}
	vp, err = vlog.Write(large)
	if err != nil {
		t.Fatalf("failed to write large value: %v", err)
	}
	if vp.Size == 0 {
		t.Error("expected non-zero size for large value")
	}

	// Чтение
	read, err := vlog.Read(vp)
	if err != nil {
		t.Fatalf("failed to read value: %v", err)
	}
	if len(read) != len(large) {
		t.Errorf("length mismatch: expected %d, got %d", len(large), len(read))
	}
	for i := range large {
		if read[i] != large[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, large[i], read[i])
		}
	}
}

func TestVLogCRCError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer vlog.Close()

	// Записываем значение
	value := []byte("test value")
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Портим данные в файле (изменяем один байт)
	file, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file for corruption: %v", err)
	}
	// Смещаемся за заголовок и CRC (8 байт) + offset
	corruptPos := vp.Offset + 8 + 5 // 5-й байт значения
	file.Seek(corruptPos, 0)
	file.Write([]byte{0xFF})
	file.Close()

	// Попытка чтения должна вернуть ошибку CRC
	_, err = vlog.Read(vp)
	if err == nil {
		t.Error("expected CRC error, got nil")
	}
}

func TestVLogReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	// Записываем значение в первый сеанс
	vlog1, err := OpenVLog(path)
	if err != nil {
		t.Fatalf("failed to open vlog1: %v", err)
	}
	// Используем большое значение, чтобы оно записалось в VLog
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog1.Write(value)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}
	if vp.Size == 0 {
		t.Fatal("expected non-zero size for large value")
	}
	vlog1.Close()

	// Открываем заново
	vlog2, err := OpenVLog(path)
	if err != nil {
		t.Fatalf("failed to open vlog2: %v", err)
	}
	defer vlog2.Close()

	// Читаем
	read, err := vlog2.Read(vp)
	if err != nil {
		t.Fatalf("failed to read after reopen: %v", err)
	}
	if string(read) != string(value) {
		t.Errorf("value mismatch: expected %s, got %s", value, read)
	}
}