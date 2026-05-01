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
	"log"
	"os"
	"scoriadb/internal/engine/vfs"
	"sort"
	"sync"
	"syscall"
)

const (
	// VLogMagic магическое число для заголовка VLog файла.
	VLogMagic uint32 = 0x53434F52 // "SCOR"
	// VLogVersion версия формата.
	VLogVersion uint32 = 1
	// MaxInlineSize максимальный размер значения для inline хранения.
	MaxInlineSize = 64
)

// ValuePointer указывает на значение в VLog.
type ValuePointer struct {
	Offset int64 // смещение в файле
	Size   int32 // размер значения (без заголовка)
}

// VLog представляет Value Log с mmap для zero-copy чтения.
type VLog struct {
	mu     sync.RWMutex
	file   *os.File
	data   []byte // mmap-регион
	size   int64  // текущий размер файла
	closed bool
}

// OpenVLog открывает или создает VLog файл.
func OpenVLog(vfs vfs.VFS, path string) (*VLog, error) {
	// Открываем файл через VFS
	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open vlog file: %w", err)
	}

	// Для mmap нужен *os.File (реальный файловый дескриптор)
	osFile, ok := file.(*os.File)
	if !ok {
		file.Close()
		return nil, fmt.Errorf("vlog mmap requires real os.File, got %T", file)
	}

	// Получаем размер файла
	stat, err := osFile.Stat()
	if err != nil {
		osFile.Close()
		return nil, fmt.Errorf("failed to stat vlog file: %w", err)
	}
	size := stat.Size()

	// Если файл пустой, записываем заголовок
	if size == 0 {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], VLogMagic)
		binary.BigEndian.PutUint32(header[4:8], VLogVersion)
		if _, err := osFile.Write(header); err != nil {
			osFile.Close()
			return nil, fmt.Errorf("failed to write vlog header: %w", err)
		}
		size = 8
	} else {
		// Проверяем заголовок
		header := make([]byte, 8)
		if _, err := osFile.ReadAt(header, 0); err != nil {
			osFile.Close()
			return nil, fmt.Errorf("failed to read vlog header: %w", err)
		}
		magic := binary.BigEndian.Uint32(header[0:4])
		version := binary.BigEndian.Uint32(header[4:8])
		if magic != VLogMagic {
			// Повреждённый VLog: логируем, удаляем файл и создаём новый
			log.Printf("vlog: magic mismatch, removing corrupted file %s", path)
			osFile.Close()
			if err := vfs.Remove(path); err != nil {
				// Попробуем переименовать файл как запасной вариант
				backupPath := path + ".corrupted"
				if renameErr := vfs.Rename(path, backupPath); renameErr != nil {
					return nil, fmt.Errorf("failed to remove corrupted vlog file (remove: %v, rename: %v)", err, renameErr)
				}
				log.Printf("vlog: renamed corrupted file to %s", backupPath)
			}
			// Открываем заново (будет создан пустой файл)
			return OpenVLog(vfs, path)
		}
		if version != VLogVersion {
			// Повреждённый VLog: логируем, удаляем файл и создаём новый
			log.Printf("vlog: version mismatch (got %d, expected %d), removing corrupted file %s", version, VLogVersion, path)
			osFile.Close()
			if err := vfs.Remove(path); err != nil {
				// Попробуем переименовать файл как запасной вариант
				backupPath := path + ".corrupted"
				if renameErr := vfs.Rename(path, backupPath); renameErr != nil {
					return nil, fmt.Errorf("failed to remove corrupted vlog file (remove: %v, rename: %v)", err, renameErr)
				}
				log.Printf("vlog: renamed corrupted file to %s", backupPath)
			}
			// Открываем заново (будет создан пустой файл)
			return OpenVLog(vfs, path)
		}
	}

	// Отображаем файл в память
	data, err := syscall.Mmap(int(osFile.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		osFile.Close()
		return nil, fmt.Errorf("failed to mmap vlog file: %w", err)
	}

	vlog := &VLog{
		file: osFile,
		data: data,
		size: size,
	}
	return vlog, nil
}

// Write добавляет значение в VLog и возвращает указатель на него.
// Если значение маленькое (<= MaxInlineSize), возвращает пустой указатель.
func (v *VLog) Write(value []byte) (ValuePointer, error) {
	if len(value) <= MaxInlineSize {
		// inline значение, не пишем в VLog
		return ValuePointer{}, nil
	}

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return ValuePointer{}, fmt.Errorf("vlog is closed")
	}

	// Вычисляем CRC32 значения
	crc := crc32.ChecksumIEEE(value)

	// Заголовок записи: CRC (4 байта), размер значения (4 байта)
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[0:4], crc)
	binary.BigEndian.PutUint32(header[4:8], uint32(len(value)))

	// Записываем заголовок и значение в файл
	offset := v.size
	if _, err := v.file.Write(header); err != nil {
		return ValuePointer{}, fmt.Errorf("failed to write vlog header: %w", err)
	}
	if _, err := v.file.Write(value); err != nil {
		return ValuePointer{}, fmt.Errorf("failed to write vlog value: %w", err)
	}
	// Синхронизируем на диск (опционально, можно отложить)
	if err := v.file.Sync(); err != nil {
		return ValuePointer{}, fmt.Errorf("failed to sync vlog: %w", err)
	}

	// Обновляем размер и перемаппируем mmap
	v.size += int64(8 + len(value))
	if err := v.remap(); err != nil {
		return ValuePointer{}, fmt.Errorf("failed to remap vlog: %w", err)
	}

	return ValuePointer{Offset: offset, Size: int32(len(value))}, nil
}

// Read читает значение по указателю.
func (v *VLog) Read(vp ValuePointer) ([]byte, error) {
	if vp.Size == 0 {
		// inline значение, не должно вызываться
		return nil, fmt.Errorf("zero-sized value pointer")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	// Проверяем границы
	start := int(vp.Offset)
	end := start + 8 + int(vp.Size) // заголовок + значение
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d", vp.Offset, vp.Size, len(v.data))
	}

	// Читаем заголовок
	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	// Читаем значение
	value := v.data[start+8 : end]

	// Проверяем CRC
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	// Возвращаем копию, чтобы избежать изменения mmap-региона
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// remap перемаппирует файл после увеличения размера.
func (v *VLog) remap() error {
	// Удаляем старое отображение
	if err := syscall.Munmap(v.data); err != nil {
		return fmt.Errorf("failed to munmap vlog: %w", err)
	}

	// Создаем новое отображение на обновленный размер
	data, err := syscall.Mmap(int(v.file.Fd()), 0, int(v.size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to remap vlog: %w", err)
	}
	v.data = data
	return nil
}

// Size возвращает текущий размер файла VLog (в байтах).
func (v *VLog) Size() int64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.size
}

// GC performs garbage collection on the Value Log.
// livePointers is a set of ValuePointers that are still referenced by the LSM tree.
// It creates a new VLog file, copies all live values to it, and replaces the old file.
// Returns a map from old ValuePointers to new ValuePointers, and any error.
// The caller must update references in the LSM tree accordingly.
func (v *VLog) GC(livePointers map[ValuePointer]struct{}) (map[ValuePointer]ValuePointer, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	// Create a temporary file for the new VLog
	tempPath := v.file.Name() + ".gc.tmp"
	file, err := os.OpenFile(tempPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp vlog file: %w", err)
	}
	defer func() {
		if file != nil {
			file.Close()
			os.Remove(tempPath)
		}
	}()

	// Write header
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[0:4], VLogMagic)
	binary.BigEndian.PutUint32(header[4:8], VLogVersion)
	if _, err := file.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write vlog header: %w", err)
	}

	// Map from old pointer to new pointer
	translation := make(map[ValuePointer]ValuePointer)
	newOffset := int64(8) // start after header

	// Iterate through live pointers in sorted order to maintain deterministic output
	// First, collect and sort pointers by offset
	var pointers []ValuePointer
	for vp := range livePointers {
		pointers = append(pointers, vp)
	}
	// Sort by offset
	sort.Slice(pointers, func(i, j int) bool {
		return pointers[i].Offset < pointers[j].Offset
	})

	// Copy each live value to the new VLog
	for _, oldVP := range pointers {
		// Read the value from the old VLog
		value, err := v.readAt(oldVP.Offset, oldVP.Size)
		if err != nil {
			return nil, fmt.Errorf("failed to read value at offset %d: %w", oldVP.Offset, err)
		}

		// Write to new VLog
		crc := crc32.ChecksumIEEE(value)
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], crc)
		binary.BigEndian.PutUint32(header[4:8], uint32(len(value)))
		if _, err := file.Write(header); err != nil {
			return nil, fmt.Errorf("failed to write vlog header: %w", err)
		}
		if _, err := file.Write(value); err != nil {
			return nil, fmt.Errorf("failed to write vlog value: %w", err)
		}

		// Record translation
		newVP := ValuePointer{Offset: newOffset, Size: oldVP.Size}
		translation[oldVP] = newVP
		newOffset += int64(8 + len(value))
	}

	// Sync the new file
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync new vlog: %w", err)
	}

	// Close the new file
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close new vlog: %w", err)
	}
	file = nil

	// Close old VLog (unmap)
	if err := syscall.Munmap(v.data); err != nil {
		return nil, fmt.Errorf("failed to munmap old vlog: %w", err)
	}
	if err := v.file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close old vlog file: %w", err)
	}

	// Replace old file with new file
	oldPath := v.file.Name()
	if err := os.Rename(tempPath, oldPath); err != nil {
		return nil, fmt.Errorf("failed to rename temp vlog: %w", err)
	}

	// Reopen the new file as the current VLog
	newFile, err := os.OpenFile(oldPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen vlog: %w", err)
	}
	stat, err := newFile.Stat()
	if err != nil {
		newFile.Close()
		return nil, fmt.Errorf("failed to stat new vlog: %w", err)
	}
	size := stat.Size()

	// Remap
	data, err := syscall.Mmap(int(newFile.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		newFile.Close()
		return nil, fmt.Errorf("failed to mmap new vlog: %w", err)
	}

	// Update VLog state
	v.file = newFile
	v.data = data
	v.size = size

	return translation, nil
}

// readAt reads a value directly from the mmap'ed data at the given offset.
// Assumes the caller holds the lock.
func (v *VLog) readAt(offset int64, size int32) ([]byte, error) {
	start := int(offset)
	end := start + 8 + int(size) // header + value
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d", offset, size, len(v.data))
	}

	// Read header
	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, size)
	}

	// Read value
	value := v.data[start+8 : end]

	// Check CRC
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	// Return a copy
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Close закрывает VLog и освобождает ресурсы.
func (v *VLog) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil
	}

	v.closed = true
	if err := syscall.Munmap(v.data); err != nil {
		return fmt.Errorf("failed to munmap vlog: %w", err)
	}
	if err := v.file.Close(); err != nil {
		return fmt.Errorf("failed to close vlog file: %w", err)
	}
	return nil
}
