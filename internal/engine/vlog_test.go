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
	"os"
	"path/filepath"
	"scoriadb/internal/engine/vfs"
	"testing"
)

func TestVLogWriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
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

func TestBasicGC(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer vlog.Close()

	// Write some large values
	value1 := make([]byte, 100)
	for i := range value1 {
		value1[i] = byte(i)
	}
	vp1, err := vlog.Write(value1)
	if err != nil {
		t.Fatalf("failed to write value1: %v", err)
	}

	value2 := make([]byte, 200)
	for i := range value2 {
		value2[i] = byte(i + 100)
	}
	vp2, err := vlog.Write(value2)
	if err != nil {
		t.Fatalf("failed to write value2: %v", err)
	}

	// Record initial size
	initialSize := vlog.Size()

	// Create a set of live pointers (simulate that only vp1 is still alive)
	livePointers := map[ValuePointer]struct{}{
		vp1: {},
	}

	// Run GC
	translation, err := vlog.GC(livePointers)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	// Check that translation map contains vp1 -> newVP
	newVP1, ok := translation[vp1]
	if !ok {
		t.Error("translation map missing vp1")
	}
	// vp2 should not be in translation (it's dead)
	if _, ok := translation[vp2]; ok {
		t.Error("translation map should not contain dead pointer vp2")
	}

	// Verify new VLog size is smaller (since we removed one value)
	newSize := vlog.Size()
	if newSize >= initialSize {
		t.Errorf("expected size to decrease after GC, got initial=%d new=%d", initialSize, newSize)
	}

	// Verify vp1 value is still readable via new pointer
	read, err := vlog.Read(newVP1)
	if err != nil {
		t.Fatalf("failed to read value after GC: %v", err)
	}
	if len(read) != len(value1) {
		t.Errorf("length mismatch: expected %d, got %d", len(value1), len(read))
	}
	for i := range value1 {
		if read[i] != value1[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, value1[i], read[i])
			break
		}
	}

	// Verify vp2 is no longer readable (should be out of range)
	_, err = vlog.Read(vp2)
	if err == nil {
		t.Error("expected error reading dead pointer vp2")
	}
}

func TestGCPreservesLatestVersions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer vlog.Close()

	// Write a value
	value1 := make([]byte, 100)
	for i := range value1 {
		value1[i] = byte(i)
	}
	vp1, err := vlog.Write(value1)
	if err != nil {
		t.Fatalf("failed to write value1: %v", err)
	}

	// Overwrite with new value (simulating update)
	value2 := make([]byte, 150)
	for i := range value2 {
		value2[i] = byte(i + 100)
	}
	vp2, err := vlog.Write(value2)
	if err != nil {
		t.Fatalf("failed to write value2: %v", err)
	}

	// Only the latest version (vp2) is live
	livePointers := map[ValuePointer]struct{}{
		vp2: {},
	}

	// Run GC
	translation, err := vlog.GC(livePointers)
	if err != nil {
		t.Fatalf("GC failed: %v", err)
	}

	// Check that only vp2 is translated
	if len(translation) != 1 {
		t.Errorf("expected 1 translation, got %d", len(translation))
	}
	newVP2, ok := translation[vp2]
	if !ok {
		t.Error("translation map missing vp2")
	}

	// Verify vp2 value is still readable
	read, err := vlog.Read(newVP2)
	if err != nil {
		t.Fatalf("failed to read value after GC: %v", err)
	}
	if len(read) != len(value2) {
		t.Errorf("length mismatch: expected %d, got %d", len(value2), len(read))
	}
	for i := range value2 {
		if read[i] != value2[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, value2[i], read[i])
			break
		}
	}

	// Old pointer vp1 should be invalid
	_, err = vlog.Read(vp1)
	if err == nil {
		t.Error("expected error reading old pointer vp1")
	}
}

func TestVLogCRCError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
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
	if _, err := file.Seek(corruptPos, 0); err != nil {
		file.Close()
		t.Fatalf("failed to seek: %v", err)
	}
	if _, err := file.Write([]byte{0xFF}); err != nil {
		file.Close()
		t.Fatalf("failed to write corruption: %v", err)
	}
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
	vlog1, err := OpenVLog(vfs.Default, path)
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
	vlog2, err := OpenVLog(vfs.Default, path)
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

func TestVLogRecoveryAfterCrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	// 1. Create a VLog and write a large value
	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}
	if vp.Size == 0 {
		t.Fatal("expected non-zero size for large value")
	}
	vlog.Close()

	// 2. Corrupt the magic number in the file
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file for corruption: %v", err)
	}
	// Write invalid magic at offset 0
	invalidMagic := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	if _, err := f.WriteAt(invalidMagic, 0); err != nil {
		f.Close()
		t.Fatalf("failed to corrupt magic: %v", err)
	}
	f.Close()

	// 3. Reopen VLog - should recover automatically (delete corrupted file and create new)
	vlog2, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to reopen vlog after corruption: %v", err)
	}
	defer vlog2.Close()

	// 4. The previous value pointer is no longer valid because the file was recreated.
	// Attempting to read should fail (out of range). We can test that.
	_, err = vlog2.Read(vp)
	if err == nil {
		t.Error("expected error reading from corrupted vlog, got nil")
	}
	// 5. Ensure we can write new values to the new VLog
	newValue := make([]byte, 100)
	for i := range newValue {
		newValue[i] = byte(i + 100) // different pattern
	}
	vp2, err := vlog2.Write(newValue)
	if err != nil {
		t.Fatalf("failed to write new value: %v", err)
	}
	if vp2.Size == 0 {
		t.Fatal("expected non-zero size for large new value")
	}
	read, err := vlog2.Read(vp2)
	if err != nil {
		t.Fatalf("failed to read new value: %v", err)
	}
	if len(read) != len(newValue) {
		t.Errorf("length mismatch: expected %d, got %d", len(newValue), len(read))
	}
	for i := range newValue {
		if read[i] != newValue[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, newValue[i], read[i])
			break
		}
	}
}
