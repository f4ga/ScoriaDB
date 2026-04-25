package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
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
func OpenVLog(path string) (*VLog, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open vlog file: %w", err)
	}

	// Получаем размер файла
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat vlog file: %w", err)
	}
	size := stat.Size()

	// Если файл пустой, записываем заголовок
	if size == 0 {
		header := make([]byte, 8)
		binary.BigEndian.PutUint32(header[0:4], VLogMagic)
		binary.BigEndian.PutUint32(header[4:8], VLogVersion)
		if _, err := file.Write(header); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to write vlog header: %w", err)
		}
		size = 8
	} else {
		// Проверяем заголовок
		header := make([]byte, 8)
		if _, err := file.ReadAt(header, 0); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read vlog header: %w", err)
		}
		magic := binary.BigEndian.Uint32(header[0:4])
		version := binary.BigEndian.Uint32(header[4:8])
		if magic != VLogMagic {
			file.Close()
			return nil, fmt.Errorf("invalid vlog magic: %x", magic)
		}
		if version != VLogVersion {
			file.Close()
			return nil, fmt.Errorf("unsupported vlog version: %d", version)
		}
	}

	// Отображаем файл в память
	data, err := syscall.Mmap(int(file.Fd()), 0, int(size), syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to mmap vlog file: %w", err)
	}

	vlog := &VLog{
		file: file,
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