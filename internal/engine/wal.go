package engine

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
)

// OpType тип операции в WAL.
type OpType byte

const (
	OpPut    OpType = 1
	OpDelete OpType = 2
)

// WalEntry представляет запись в WAL.
type WalEntry struct {
	Op        OpType
	Key       []byte
	Value     []byte
	Timestamp uint64
}

// WAL представляет Write-Ahead Log с CRC.
type WAL struct {
	mu     sync.Mutex
	file   *os.File
	offset int64 // текущая позиция записи
}

// OpenWAL открывает или создает WAL файл.
func OpenWAL(path string) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open wal file: %w", err)
	}

	// Получаем текущий размер файла
	stat, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to stat wal file: %w", err)
	}

	wal := &WAL{
		file:   file,
		offset: stat.Size(),
	}
	return wal, nil
}

// Write записывает операцию в WAL.
func (w *WAL) Write(entry *WalEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Сериализуем запись
	buf, err := encodeWalEntry(entry)
	if err != nil {
		return fmt.Errorf("failed to encode wal entry: %w", err)
	}

	// Записываем в файл
	n, err := w.file.Write(buf)
	if err != nil {
		return fmt.Errorf("failed to write wal entry: %w", err)
	}
	// Синхронизируем на диск для гарантии durability
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync wal: %w", err)
	}

	w.offset += int64(n)
	return nil
}

// Recover читает все записи из WAL и вызывает callback для каждой.
func (w *WAL) Recover(cb func(*WalEntry) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Перемещаемся в начало файла
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("failed to seek wal: %w", err)
	}

	reader := io.Reader(w.file)
	for {
		entry, err := decodeWalEntry(reader)
		if err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("failed to decode wal entry: %w", err)
		}
		if err := cb(entry); err != nil {
			return fmt.Errorf("callback error: %w", err)
		}
	}
	return nil
}

// Close закрывает WAL файл.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// encodeWalEntry сериализует запись в байты с CRC.
func encodeWalEntry(entry *WalEntry) ([]byte, error) {
	// Размеры
	keyLen := len(entry.Key)
	valLen := len(entry.Value)
	// Общий размер: тип (1) + timestamp (8) + keyLen (2) + valLen (4) + ключ + значение
	totalSize := 1 + 8 + 2 + 4 + keyLen + valLen

	// Буфер
	buf := make([]byte, totalSize+4) // +4 для CRC
	pos := 0

	buf[pos] = byte(entry.Op)
	pos++

	binary.BigEndian.PutUint64(buf[pos:pos+8], entry.Timestamp)
	pos += 8

	binary.BigEndian.PutUint16(buf[pos:pos+2], uint16(keyLen))
	pos += 2

	binary.BigEndian.PutUint32(buf[pos:pos+4], uint32(valLen))
	pos += 4

	copy(buf[pos:pos+keyLen], entry.Key)
	pos += keyLen

	copy(buf[pos:pos+valLen], entry.Value)
	pos += valLen

	// Вычисляем CRC для данных (без CRC поля)
	crc := crc32.ChecksumIEEE(buf[:totalSize])
	binary.BigEndian.PutUint32(buf[pos:pos+4], crc)

	return buf, nil
}

// decodeWalEntry читает запись из потока.
func decodeWalEntry(r io.Reader) (*WalEntry, error) {
	// Читаем заголовок (без CRC)
	header := make([]byte, 1+8+2+4)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	op := OpType(header[0])
	timestamp := binary.BigEndian.Uint64(header[1:9])
	keyLen := binary.BigEndian.Uint16(header[9:11])
	valLen := binary.BigEndian.Uint32(header[11:15])

	// Читаем ключ и значение
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	value := make([]byte, valLen)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil, err
	}

	// Читаем CRC
	crcBuf := make([]byte, 4)
	if _, err := io.ReadFull(r, crcBuf); err != nil {
		return nil, err
	}
	crcStored := binary.BigEndian.Uint32(crcBuf)

	// Проверяем CRC
	// Собираем данные для проверки
	data := make([]byte, 1+8+2+4+int(keyLen)+int(valLen))
	copy(data[0:], header)
	copy(data[1+8+2+4:], key)
	copy(data[1+8+2+4+int(keyLen):], value)
	crc := crc32.ChecksumIEEE(data)
	if crc != crcStored {
		return nil, fmt.Errorf("crc mismatch: stored=%x, computed=%x", crcStored, crc)
	}

	return &WalEntry{
		Op:        op,
		Key:       key,
		Value:     value,
		Timestamp: timestamp,
	}, nil
}