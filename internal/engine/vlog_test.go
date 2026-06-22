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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
)

func TestVLogWriteRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

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
	defer errors.CloseWithFatal(vlog, "vlog")

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
	defer errors.CloseWithFatal(vlog, "vlog")

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
	defer errors.CloseWithFatal(vlog, "vlog")

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
		errors.CloseWithFatal(file, "vlog-corrupt-file")
		t.Fatalf("failed to seek: %v", err)
	}
	if _, err := file.Write([]byte{0xFF}); err != nil {
		errors.CloseWithFatal(file, "vlog-corrupt-file")
		t.Fatalf("failed to write corruption: %v", err)
	}
	errors.CloseWithFatal(file, "vlog-corrupt-file")

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
	errors.CloseWithFatal(vlog1, "vlog1")

	// Открываем заново
	vlog2, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog2: %v", err)
	}
	defer errors.CloseWithFatal(vlog2, "vlog2")

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
	errors.CloseWithFatal(vlog, "vlog")

	// 2. Corrupt the magic number in the file
	f, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file for corruption: %v", err)
	}
	// Write invalid magic at offset 0
	invalidMagic := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	if _, err := f.WriteAt(invalidMagic, 0); err != nil {
		errors.CloseWithFatal(f, "vlog-corrupt-file")
		t.Fatalf("failed to corrupt magic: %v", err)
	}
	errors.CloseWithFatal(f, "vlog-corrupt-file")

	// 3. Reopen VLog - should recover automatically (delete corrupted file and create new)
	vlog2, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to reopen vlog after corruption: %v", err)
	}
	defer errors.CloseWithFatal(vlog2, "vlog2")

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

func TestVLogRefCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// Initial refCount should be 0
	if atomic.LoadInt32(&vlog.refCount) != 0 {
		t.Errorf("expected initial refCount 0, got %d", vlog.refCount)
	}

	// IncRef should increase refCount
	vlog.IncRef()
	if atomic.LoadInt32(&vlog.refCount) != 1 {
		t.Errorf("expected refCount 1 after IncRef, got %d", vlog.refCount)
	}

	// IncRef again
	vlog.IncRef()
	if atomic.LoadInt32(&vlog.refCount) != 2 {
		t.Errorf("expected refCount 2 after second IncRef, got %d", vlog.refCount)
	}

	// DecRef should decrease refCount (but NOT close the file)
	vlog.DecRef()
	if atomic.LoadInt32(&vlog.refCount) != 1 {
		t.Errorf("expected refCount 1 after DecRef, got %d", vlog.refCount)
	}

	// Close should block until refCount == 0, then close
	// Since refCount is 1, we need to DecRef in a goroutine
	go vlog.DecRef()
	// Close will wait for refCount to reach 0
	if err := vlog.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After Close, vlog should be closed
	if !vlog.closed {
		t.Error("expected vlog to be closed after Close()")
	}
}

func TestVLogRefCountConcurrent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// Concurrent IncRef/DecRef from multiple goroutines
	const goroutines = 10
	const iterations = 100
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				vlog.IncRef()
				vlog.DecRef()
			}
		}()
	}
	wg.Wait()

	// After all operations, refCount should be 0
	if atomic.LoadInt32(&vlog.refCount) != 0 {
		t.Errorf("expected refCount 0 after concurrent ops, got %d", vlog.refCount)
	}
}
func TestVLogReaderInterface(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// VLogImpl should implement VLogReader
	var reader VLogReader = vlog

	// Write a large value
	large := make([]byte, 100)
	for i := range large {
		large[i] = byte(i)
	}
	vp, err := vlog.Write(large)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Read through the interface
	read, err := reader.Read(vp)
	if err != nil {
		t.Fatalf("failed to read through interface: %v", err)
	}
	if len(read) != len(large) {
		t.Errorf("length mismatch: expected %d, got %d", len(large), len(read))
	}

	// Size through the interface
	if reader.Size() <= 0 {
		t.Error("expected positive size")
	}
}

func TestVLogViewBasic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// Write a large value (will be stored in VLog)
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

	// ReadView should return a zero-copy view
	view, err := vlog.ReadView(vp)
	if err != nil {
		t.Fatalf("ReadView failed: %v", err)
	}
	if view == nil {
		t.Fatal("expected non-nil view")
	}

	// Data should match the original
	read := view.Data()
	if len(read) != len(value) {
		t.Errorf("length mismatch: expected %d, got %d", len(value), len(read))
	}
	for i := range value {
		if read[i] != value[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, value[i], read[i])
		}
	}

	// Release the view
	view.Release()

	// After Release, view.Data() should return nil
	if view.Data() != nil {
		t.Error("expected nil data after Release")
	}
}

func TestVLogViewRefCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// Write a large value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Initial refCount should be 0
	if atomic.LoadInt32(&vlog.refCount) != 0 {
		t.Errorf("expected initial refCount 0, got %d", vlog.refCount)
	}

	// Create a view — refCount should increase
	view1, err := vlog.ReadView(vp)
	if err != nil {
		t.Fatalf("ReadView failed: %v", err)
	}
	if atomic.LoadInt32(&vlog.refCount) != 1 {
		t.Errorf("expected refCount 1 after ReadView, got %d", vlog.refCount)
	}

	// Create another view — refCount should increase to 2
	view2, err := vlog.ReadView(vp)
	if err != nil {
		t.Fatalf("second ReadView failed: %v", err)
	}
	if atomic.LoadInt32(&vlog.refCount) != 2 {
		t.Errorf("expected refCount 2 after second ReadView, got %d", vlog.refCount)
	}

	// Release first view — refCount should decrease to 1
	view1.Release()
	if atomic.LoadInt32(&vlog.refCount) != 1 {
		t.Errorf("expected refCount 1 after first Release, got %d", vlog.refCount)
	}

	// Release second view — refCount should decrease to 0
	view2.Release()
	if atomic.LoadInt32(&vlog.refCount) != 0 {
		t.Errorf("expected refCount 0 after second Release, got %d", vlog.refCount)
	}

	// Close should succeed when refCount == 0
	if err := vlog.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestVLogViewCRCError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// Write a value
	value := []byte("test value for CRC check")
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Corrupt the data in the file
	file, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file for corruption: %v", err)
	}
	corruptPos := vp.Offset + 8 + 5 // 5th byte of the value
	if _, err := file.Seek(corruptPos, 0); err != nil {
		errors.CloseWithFatal(file, "vlog-corrupt-file")
		t.Fatalf("failed to seek: %v", err)
	}
	if _, err := file.Write([]byte{0xFF}); err != nil {
		errors.CloseWithFatal(file, "vlog-corrupt-file")
		t.Fatalf("failed to write corruption: %v", err)
	}
	errors.CloseWithFatal(file, "vlog-corrupt-file")

	// ReadView should fail with CRC error
	_, err = vlog.ReadView(vp)
	if err == nil {
		t.Error("expected CRC error from ReadView, got nil")
	}
}

func TestVLogViewOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// ReadView with invalid pointer should fail
	_, err = vlog.ReadView(ValuePointer{Offset: 999999, Size: 100})
	if err == nil {
		t.Error("expected error for out-of-range pointer, got nil")
	}
}

func TestVLogViewDataRemainsValidUntilRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}
	defer errors.CloseWithFatal(vlog, "vlog")

	// Write a large value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Get a zero-copy view
	view, err := vlog.ReadView(vp)
	if err != nil {
		t.Fatalf("ReadView failed: %v", err)
	}

	// Data should be valid while view is held
	data := view.Data()
	if len(data) != len(value) {
		t.Fatalf("length mismatch: expected %d, got %d", len(value), len(data))
	}
	for i := range value {
		if data[i] != value[i] {
			t.Fatalf("byte mismatch at index %d: expected %d, got %d", i, value[i], data[i])
		}
	}

	// Write more data to VLog (this triggers remap, but our view should still be valid
	// because we hold a reference)
	extraValue := make([]byte, 200)
	for i := range extraValue {
		extraValue[i] = byte(i + 200)
	}
	_, err = vlog.Write(extraValue)
	if err != nil {
		t.Fatalf("failed to write extra value: %v", err)
	}

	// Our view data should still be valid (we hold refCount)
	if len(data) != len(value) {
		t.Errorf("data length changed after write: expected %d, got %d", len(value), len(data))
	}
	for i := range value {
		if data[i] != value[i] {
			t.Errorf("byte mismatch at index %d after write: expected %d, got %d", i, value[i], data[i])
			break
		}
	}

	// Release the view
	view.Release()
}

func TestVLogViewConcurrentReads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}

	// Write a value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Concurrent ReadView and Release from multiple goroutines
	const goroutines = 10
	const iterations = 50
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				view, err := vlog.ReadView(vp)
				if err != nil {
					t.Errorf("ReadView failed: %v", err)
					return
				}
				// Verify data is valid while holding the view
				data := view.Data()
				if len(data) != len(value) {
					t.Errorf("length mismatch: expected %d, got %d", len(value), len(data))
					view.Release()
					return
				}
				for k := range value {
					if data[k] != value[k] {
						t.Errorf("byte mismatch at index %d", k)
						view.Release()
						return
					}
				}
				// Release the view — DecRef() does NOT close the file
				view.Release()
			}
		}()
	}
	wg.Wait()

	// After all goroutines, refCount should be 0
	if atomic.LoadInt32(&vlog.refCount) != 0 {
		t.Errorf("expected refCount 0 after concurrent reads, got %d", vlog.refCount)
	}

	// Close vlog after all goroutines are done — Close() waits for refCount == 0
	if err := vlog.Close(); err != nil {
		t.Fatalf("failed to close vlog: %v", err)
	}
}

func TestVLogShutdownGraceful(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}

	// Write a large value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Create a view (holds a reference)
	view, err := vlog.ReadView(vp)
	if err != nil {
		t.Fatalf("ReadView failed: %v", err)
	}

	// Shutdown should wait for the view to be released
	// Release the view in a goroutine after a short delay
	go func() {
		time.Sleep(50 * time.Millisecond)
		view.Release()
	}()

	// Shutdown with 1 second timeout (should succeed)
	if err := vlog.Shutdown(1 * time.Second); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	// After shutdown, vlog should be closed
	if !vlog.closed {
		t.Error("expected vlog to be closed after Shutdown")
	}
}

func TestVLogShutdownTimeout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}

	// Write a large value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Create a view (holds a reference) — do NOT release it
	view, err := vlog.ReadView(vp)
	if err != nil {
		t.Fatalf("ReadView failed: %v", err)
	}
	defer view.Release()

	// Shutdown with very short timeout (should timeout)
	err = vlog.Shutdown(10 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout error from Shutdown, got nil")
	}
}

func TestVLogShutdownNoViews(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}

	// Write a large value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	_, err = vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Shutdown with no active views (should succeed immediately)
	if err := vlog.Shutdown(1 * time.Second); err != nil {
		t.Fatalf("Shutdown failed: %v", err)
	}

	if !vlog.closed {
		t.Error("expected vlog to be closed after Shutdown")
	}
}

func TestVLogShutdownRejectsNewViews(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vlog.db")

	vlog, err := OpenVLog(vfs.Default, path)
	if err != nil {
		t.Fatalf("failed to open vlog: %v", err)
	}

	// Write a large value
	value := make([]byte, 100)
	for i := range value {
		value[i] = byte(i)
	}
	vp, err := vlog.Write(value)
	if err != nil {
		t.Fatalf("failed to write value: %v", err)
	}

	// Start shutdown in a goroutine (it will wait because no views are held)
	go func() {
		if err := vlog.Shutdown(1 * time.Second); err != nil {
			t.Errorf("Shutdown failed: %v", err)
		}
	}()

	// Give shutdown time to set closing flag
	time.Sleep(10 * time.Millisecond)

	// ReadView should fail because vlog is closing
	_, err = vlog.ReadView(vp)
	if err == nil {
		t.Error("expected error from ReadView during shutdown, got nil")
	}
}
