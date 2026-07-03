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
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

const (
	VLogMagic   uint32 = 0x53434F52
	VLogVersion uint32 = 1
	// VLogExtendSize — размер, на который расширяется mmap за один раз.
	// 512MB —大幅но减少 syscall'ов (1 раз на 131K записей по 4KB).
	// Для продакшена с большими значениями можно увеличить до 1GB.
	VLogExtendSize int64 = 512 * 1024 * 1024
)

// MaxInlineSize — максимальный размер значения, которое хранится inline (в MemTable),
// а не в Value Log. Сделано var, а не const, чтобы бенчмарки могли временно
// изменять его для тестирования различных сценариев.
// Значение по умолчанию: 64 байта.
var MaxInlineSize int = 64

type VLogReader interface {
	Read(vp ValuePointer) ([]byte, error)
	Size() int64
	Close() error
}

// VLogImpl — структура, где каждый байт на счету.
type VLogImpl struct {
	mu        sync.RWMutex
	file      *os.File
	data      []byte // mmap-регион, READ-ONLY для чтения
	writeData []byte // mmap-регион, READ-WRITE для записи
	fileSize  int64  // размер файла на диске (выровненный)
	dataSize  int64  // логический размер данных (сколько байт записано)
	mmapSize  int64  // размер mmap-региона (всегда >= fileSize)
	closed    bool
	refCount  int32
	closing   bool
	waitGroup sync.WaitGroup
	syncMode  bool
	extendCh  chan struct{} // сигнал для расширения mmap
}

var _ VLogReader = (*VLogImpl)(nil)

// OpenVLog открывает VLog с предварительным выделением mmap.
func OpenVLog(vfs vfs.VFS, path string) (*VLogImpl, error) {
	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open vlog file: %w", err)
	}

	osFile, ok := file.(*os.File)
	if !ok {
		errors.CloseWithLog(file, "vlog-file")
		return nil, fmt.Errorf("vlog mmap requires real os.File, got %T", file)
	}

	stat, err := osFile.Stat()
	if err != nil {
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to stat vlog file: %w", err)
	}

	fileSize := stat.Size()
	var dataSize int64

	// Если файл пустой или повреждён — создаём новый
	if fileSize == 0 {
		// Записываем заголовок
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], VLogMagic)
		binary.BigEndian.PutUint32(header[4:8], VLogVersion)
		if _, err := osFile.Write(header); err != nil {
			errors.CloseWithLog(osFile, "vlog-osfile")
			return nil, fmt.Errorf("failed to write vlog header: %w", err)
		}
		dataSize = 8
		fileSize = 8
	} else {
		header := make([]byte, 8)
		if _, err := osFile.ReadAt(header, 0); err != nil {
			errors.CloseWithLog(osFile, "vlog-osfile")
			return nil, fmt.Errorf("failed to read vlog header: %w", err)
		}
		magic := binary.BigEndian.Uint32(header[0:4])
		version := binary.BigEndian.Uint32(header[4:8])
		if magic != VLogMagic || version != VLogVersion {
			logger.Warn("vlog: corrupted file, recreating %s", path)
			errors.CloseWithLog(osFile, "vlog-osfile")
			if err := vfs.Remove(path); err != nil {
				backupPath := path + ".corrupted"
				if renameErr := vfs.Rename(path, backupPath); renameErr != nil {
					return nil, fmt.Errorf("failed to remove corrupted vlog file (remove: %v, rename: %v)", err, renameErr)
				}
				logger.Info("vlog: renamed corrupted file to %s", backupPath)
			}
			return OpenVLog(vfs, path)
		}
		dataSize = fileSize
	}

	// Выравниваем размер для mmap
	mmapSize := alignTo(dataSize, VLogExtendSize)
	if mmapSize < VLogExtendSize {
		mmapSize = VLogExtendSize
	}

	// Расширяем файл до выровненного размера
	if err := osFile.Truncate(mmapSize); err != nil {
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to truncate vlog to %d: %w", mmapSize, err)
	}

	// READ-ONLY mmap (для чтения через Read и ReadView)
	readData, err := syscall.Mmap(int(osFile.Fd()), 0, int(mmapSize), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to mmap vlog (read): %w", err)
	}

	// READ-WRITE mmap (для записи через Write)
	writeData, err := syscall.Mmap(int(osFile.Fd()), 0, int(mmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Munmap(readData)
		errors.CloseWithLog(osFile, "vlog-osfile")
		return nil, fmt.Errorf("failed to mmap vlog (write): %w", err)
	}

	vlog := &VLogImpl{
		file:      osFile,
		data:      readData,
		writeData: writeData,
		fileSize:  fileSize,
		dataSize:  dataSize,
		mmapSize:  mmapSize,
		extendCh:  make(chan struct{}, 1),
	}

	return vlog, nil
}

// crc32Copy копирует данные из src в dst и одновременно вычисляет CRC32.
// Это устраняет двойной проход: crc32.ChecksumIEEE() + copy().
// copy() выполняется первым, затем CRC вычисляется на dst (уже в mmap).
// Это всё равно один проход по данным из L1/L2 кэша, а не из DRAM.
//
//go:nosplit
func crc32Copy(dst, src []byte) uint32 {
	n := copy(dst, src)
	return crc32.ChecksumIEEE(dst[:n])
}

// Write — zero-syscall запись в mmap (кроме случаев расширения).
// Оптимизация: CRC вычисляется ВО ВРЕМЯ копирования (один проход вместо двух).
func (v *VLogImpl) Write(value []byte) (ValuePointer, error) {
	if len(value) <= MaxInlineSize {
		return ValuePointer{}, nil
	}

	// Быстрая проверка без захвата мьютекса
	if v.closed {
		return ValuePointer{}, fmt.Errorf("vlog is closed")
	}

	totalSize := int64(8 + len(value))

	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return ValuePointer{}, fmt.Errorf("vlog is closed")
	}

	// Проверяем, хватает ли места в mmap
	newDataSize := v.dataSize + totalSize
	if newDataSize > v.mmapSize {
		// Не хватает — расширяем mmap (это единственный syscall в этом методе)
		if err := v.extendMmap(newDataSize); err != nil {
			return ValuePointer{}, err
		}
	}

	// Расчёт смещения — быстрая операция
	offset := v.dataSize

	// Записываем размер в заголовок (CRC будет заполнен после копирования)
	binary.BigEndian.PutUint32(v.writeData[offset+4:offset+8], uint32(len(value)))

	// Копируем значение в mmap и ОДНОВРЕМЕННО считаем CRC — один проход!
	dst := v.writeData[offset+8 : offset+8+int64(len(value))]
	crc := crc32Copy(dst, value)

	// Записываем CRC в заголовок
	binary.BigEndian.PutUint32(v.writeData[offset:offset+4], crc)

	// Обновляем логический размер
	v.dataSize = newDataSize

	// Если syncMode включён — синхронизируем на диск
	if v.syncMode {
		if err := v.file.Sync(); err != nil {
			return ValuePointer{}, fmt.Errorf("failed to sync vlog: %w", err)
		}
	}

	return ValuePointer{Offset: offset, Size: int32(len(value))}, nil
}

// extendMmap — расширяет mmap и файл на VLogExtendSize.
// ВЫЗЫВАЕТСЯ ТОЛЬКО КОГДА НЕ ХВАТАЕТ МЕСТА.
func (v *VLogImpl) extendMmap(newDataSize int64) error {
	// Расчёт нового размера mmap
	newMmapSize := alignTo(newDataSize, VLogExtendSize)

	// Расширяем файл на диске
	if err := v.file.Truncate(newMmapSize); err != nil {
		return fmt.Errorf("failed to truncate vlog to %d: %w", newMmapSize, err)
	}

	// Перемаппиваем READ-ONLY регион
	oldReadData := v.data
	newReadData, err := syscall.Mmap(int(v.file.Fd()), 0, int(newMmapSize), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return fmt.Errorf("failed to remap vlog (read): %w", err)
	}
	if err := syscall.Munmap(oldReadData); err != nil {
		logger.Warn("vlog: failed to munmap old read data: %v", err)
	}

	// Перемаппиваем READ-WRITE регион
	oldWriteData := v.writeData
	newWriteData, err := syscall.Mmap(int(v.file.Fd()), 0, int(newMmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Munmap(newReadData)
		return fmt.Errorf("failed to remap vlog (write): %w", err)
	}
	if err := syscall.Munmap(oldWriteData); err != nil {
		logger.Warn("vlog: failed to munmap old write data: %v", err)
	}

	// Обновляем указатели
	v.data = newReadData
	v.writeData = newWriteData
	v.mmapSize = newMmapSize
	v.fileSize = newMmapSize

	return nil
}

// Read — копирует данные (для внешнего API).
func (v *VLogImpl) Read(vp ValuePointer) ([]byte, error) {
	if vp.Size == 0 {
		return nil, fmt.Errorf("zero-sized value pointer")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	// Копируем — для внешнего API безопасность важнее скорости
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// ReadDirect — zero-copy чтение для внутреннего использования (без копирования).
// Данные НЕЛЬЗЯ изменять! Используется только внутри движка.
// Вызывающий должен гарантировать, что VLog не будет закрыт во время использования.
func (v *VLogImpl) ReadDirect(vp ValuePointer) ([]byte, error) {
	if vp.Size == 0 {
		return nil, fmt.Errorf("zero-sized value pointer")
	}

	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	// Возвращаем прямой срез на mmap — БЕЗ КОПИРОВАНИЯ!
	return value, nil
}

// ReadView — zero-copy чтение (для внутреннего использования с ref-counting).
func (v *VLogImpl) ReadView(vp ValuePointer) (*VLogView, error) {
	v.mu.RLock()
	defer v.mu.RUnlock()

	if v.closed {
		return nil, fmt.Errorf("vlog is closed")
	}
	if v.closing {
		return nil, fmt.Errorf("vlog is closing")
	}

	start := int(vp.Offset)
	end := start + 8 + int(vp.Size)
	if end > len(v.data) {
		return nil, fmt.Errorf("value pointer out of range: offset=%d size=%d data len=%d",
			vp.Offset, vp.Size, len(v.data))
	}

	crcStored := binary.BigEndian.Uint32(v.data[start : start+4])
	sizeStored := binary.BigEndian.Uint32(v.data[start+4 : start+8])
	if sizeStored != uint32(vp.Size) {
		return nil, fmt.Errorf("size mismatch: stored=%d, pointer=%d", sizeStored, vp.Size)
	}

	value := v.data[start+8 : end]
	crc := crc32.ChecksumIEEE(value)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	v.IncRef()
	v.waitGroup.Add(1)

	return &VLogView{
		vlog: v,
		data: value,
		vp:   vp,
	}, nil
}

// VLogView — zero-copy view.
type VLogView struct {
	vlog *VLogImpl
	data []byte
	vp   ValuePointer
}

func (v *VLogView) Release() {
	if v.vlog != nil {
		v.vlog.DecRef()
		v.vlog.waitGroup.Done()
		v.vlog = nil
		v.data = nil
	}
}

func (v *VLogView) Data() []byte {
	return v.data
}

// IncRef / DecRef — атомарные операции.
func (v *VLogImpl) IncRef() {
	atomic.AddInt32(&v.refCount, 1)
}

func (v *VLogImpl) DecRef() {
	newRef := atomic.AddInt32(&v.refCount, -1)
	if newRef < 0 {
		panic("DecRef called more times than IncRef")
	}
}

// Size возвращает логический размер данных.
func (v *VLogImpl) Size() int64 {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.dataSize
}

// GC performs garbage collection on the Value Log.
// livePointers is a set of ValuePointers that are still referenced by the LSM tree.
// It creates a new VLog file, copies all live values to it, and replaces the old file.
// Returns a map from old ValuePointers to new ValuePointers, and any error.
func (v *VLogImpl) GC(livePointers map[ValuePointer]struct{}) (map[ValuePointer]ValuePointer, error) {
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
			errors.CloseWithLog(file, "vlog-gc-temp")
			errors.RemoveWithLog(tempPath)
		}
	}()

	// Write header
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header[0:4], VLogMagic)
	binary.BigEndian.PutUint32(header[4:8], VLogVersion)
	if _, err := file.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write vlog header: %w", err)
	}

	// Collect and sort pointers by offset
	var pointers []ValuePointer
	for vp := range livePointers {
		if vp.Size > 0 { // only VLog-stored values
			pointers = append(pointers, vp)
		}
	}
	sort.Slice(pointers, func(i, j int) bool {
		return pointers[i].Offset < pointers[j].Offset
	})

	translation := make(map[ValuePointer]ValuePointer)
	newOffset := int64(8)

	for _, oldVP := range pointers {
		// Read the value from the old VLog using ReadDirect (zero-copy)
		value, err := v.ReadDirect(oldVP)
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

		translation[oldVP] = ValuePointer{Offset: newOffset, Size: oldVP.Size}
		newOffset += int64(8 + len(value))
	}

	// Sync and close the new file
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("failed to sync new vlog: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close new vlog: %w", err)
	}
	file = nil

	// Close old VLog (unmap)
	if err := syscall.Munmap(v.data); err != nil {
		return nil, fmt.Errorf("failed to munmap old vlog: %w", err)
	}
	if err := syscall.Munmap(v.writeData); err != nil {
		return nil, fmt.Errorf("failed to munmap old write vlog: %w", err)
	}
	if err := v.file.Close(); err != nil {
		return nil, fmt.Errorf("failed to close old vlog file: %w", err)
	}

	// Replace old file with new file
	oldPath := v.file.Name()
	if err := os.Rename(tempPath, oldPath); err != nil {
		return nil, fmt.Errorf("failed to rename temp vlog: %w", err)
	}

	// Reopen the new file
	newFile, err := os.OpenFile(oldPath, os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen vlog: %w", err)
	}
	stat, err := newFile.Stat()
	if err != nil {
		errors.CloseWithLog(newFile, "vlog-gc-newfile")
		return nil, fmt.Errorf("failed to stat new vlog: %w", err)
	}
	newSize := stat.Size()

	// mmap the new file
	readData, err := syscall.Mmap(int(newFile.Fd()), 0, int(newSize), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		errors.CloseWithLog(newFile, "vlog-gc-newfile")
		return nil, fmt.Errorf("failed to mmap new vlog: %w", err)
	}
	writeData, err := syscall.Mmap(int(newFile.Fd()), 0, int(newSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		_ = syscall.Munmap(readData)
		errors.CloseWithLog(newFile, "vlog-gc-newfile")
		return nil, fmt.Errorf("failed to mmap write new vlog: %w", err)
	}

	// Update VLog state
	v.file = newFile
	v.data = readData
	v.writeData = writeData
	v.fileSize = newSize
	v.dataSize = newSize
	v.mmapSize = newSize

	return translation, nil
}

// Shutdown — graceful shutdown.
func (v *VLogImpl) Shutdown(timeout time.Duration) error {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return nil
	}
	v.closing = true
	v.mu.Unlock()

	done := make(chan struct{})
	go func() {
		v.waitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("VLog shutdown completed gracefully")
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.closed {
			return nil
		}
		v.closed = true
		if err := syscall.Munmap(v.data); err != nil {
			return fmt.Errorf("failed to munmap vlog: %w", err)
		}
		if err := syscall.Munmap(v.writeData); err != nil {
			return fmt.Errorf("failed to munmap write vlog: %w", err)
		}
		v.data = nil
		v.writeData = nil
		if err := v.file.Close(); err != nil {
			return fmt.Errorf("failed to close vlog file: %w", err)
		}
		return nil
	case <-time.After(timeout):
		logger.Warn("VLog shutdown timeout after %v, forcing close", timeout)
		v.mu.Lock()
		defer v.mu.Unlock()
		if !v.closed {
			v.closed = true
			_ = syscall.Munmap(v.data)
			_ = syscall.Munmap(v.writeData)
			v.data = nil
			v.writeData = nil
			_ = v.file.Close()
		}
		return fmt.Errorf("shutdown timeout after %v", timeout)
	}
}

// Close — закрывает VLog немедленно.
// В отличие от Shutdown, Close не ждёт освобождения активных view.
// Вызывающий код должен гарантировать, что все view отпущены до вызова Close,
// либо использовать Shutdown с таймаутом для graceful shutdown.
func (v *VLogImpl) Close() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.closed {
		return nil
	}
	v.closed = true

	if err := syscall.Munmap(v.data); err != nil {
		return fmt.Errorf("failed to munmap vlog: %w", err)
	}
	if err := syscall.Munmap(v.writeData); err != nil {
		return fmt.Errorf("failed to munmap write vlog: %w", err)
	}
	v.data = nil
	v.writeData = nil
	if err := v.file.Close(); err != nil {
		return fmt.Errorf("failed to close vlog file: %w", err)
	}
	return nil
}

// alignTo — выравнивает size до кратного align.
// Используется для минимизации количества syscall'ов.
func alignTo(size, align int64) int64 {
	if size%align == 0 {
		return size
	}
	return ((size / align) + 1) * align
}
