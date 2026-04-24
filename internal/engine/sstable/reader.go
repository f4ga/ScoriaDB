package sstable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"scoriadb/internal/mvcc"
)

// Reader читает SSTable из файла.
type Reader struct {
	file   *os.File
	footer Footer

	// Кэшированные данные
	indexEntries []IndexEntry
	bloomFilter  *BloomFilter

	// Диапазонные ключи
	minKey []byte
	maxKey []byte
}

// IndexEntry представляет запись в индексе блоков.
type IndexEntry struct {
	key    []byte
	offset uint64
}

// Open открывает SSTable для чтения.
func Open(path string) (*Reader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open SSTable file: %w", err)
	}

	// Читаем футер (последние 80 байт)
	if _, err := file.Seek(-80, io.SeekEnd); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to seek to footer: %w", err)
	}
	var footer Footer
	if err := binary.Read(file, binary.LittleEndian, &footer); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read footer: %w", err)
	}
	if footer.Magic != MagicNumber {
		file.Close()
		return nil, fmt.Errorf("invalid SSTable magic number")
	}

	// Читаем индекс блоков
	if _, err := file.Seek(int64(footer.IndexOffset), io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to seek to index: %w", err)
	}
	indexEntries := make([]IndexEntry, 0)
	remaining := footer.IndexSize
	for remaining > 0 {
		// Читаем длину ключа
		var keyLen uint32
		if err := binary.Read(file, binary.LittleEndian, &keyLen); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read key length: %w", err)
		}
		remaining -= 4
		// Читаем ключ
		key := make([]byte, keyLen)
		if _, err := io.ReadFull(file, key); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read key: %w", err)
		}
		remaining -= uint64(keyLen)
		// Читаем смещение блока
		var offset uint64
		if err := binary.Read(file, binary.LittleEndian, &offset); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read block offset: %w", err)
		}
		remaining -= 8
		indexEntries = append(indexEntries, IndexEntry{key: key, offset: offset})
	}

	// Читаем фильтр Блума
	if _, err := file.Seek(int64(footer.BloomOffset), io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to seek to bloom filter: %w", err)
	}
	var bloomSize uint32
	if err := binary.Read(file, binary.LittleEndian, &bloomSize); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read bloom filter size: %w", err)
	}
	bloomBytes := make([]byte, bloomSize)
	if _, err := io.ReadFull(file, bloomBytes); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to read bloom filter: %w", err)
	}
	bloomFilter := DecodeBloomFilter(bloomBytes, 3) // k = 3

	// Читаем минимальный ключ
	var minKey []byte
	if footer.MinKeyLength > 0 {
		if _, err := file.Seek(int64(footer.MinKeyOffset), io.SeekStart); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to seek to min key: %w", err)
		}
		var keyLen uint32
		if err := binary.Read(file, binary.LittleEndian, &keyLen); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read min key length: %w", err)
		}
		minKey = make([]byte, keyLen)
		if _, err := io.ReadFull(file, minKey); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read min key: %w", err)
		}
	}

	// Читаем максимальный ключ
	var maxKey []byte
	if footer.MaxKeyLength > 0 {
		if _, err := file.Seek(int64(footer.MaxKeyOffset), io.SeekStart); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to seek to max key: %w", err)
		}
		var keyLen uint32
		if err := binary.Read(file, binary.LittleEndian, &keyLen); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read max key length: %w", err)
		}
		maxKey = make([]byte, keyLen)
		if _, err := io.ReadFull(file, maxKey); err != nil {
			file.Close()
			return nil, fmt.Errorf("failed to read max key: %w", err)
		}
	}

	return &Reader{
		file:         file,
		footer:       footer,
		indexEntries: indexEntries,
		bloomFilter:  bloomFilter,
		minKey:       minKey,
		maxKey:       maxKey,
	}, nil
}

// Lookup ищет ключ в SSTable и возвращает значение, если найдено.
func (r *Reader) Lookup(key mvcc.MVCCKey) ([]byte, bool) {
	userKey := key.Key

	// Диапазонный фильтр: если ключ вне диапазона min/max, сразу возвращаем false
	if len(r.minKey) > 0 && bytes.Compare(userKey, r.minKey) < 0 {
		return nil, false
	}
	if len(r.maxKey) > 0 && bytes.Compare(userKey, r.maxKey) > 0 {
		return nil, false
	}

	// Проверяем фильтр Блума (если ключа нет, пропускаем)
	if !r.bloomFilter.MayContain(userKey) {
		return nil, false
	}

	// Находим блок, который может содержать ключ
	blockIndex := -1
	for i, entry := range r.indexEntries {
		// Декодируем ключ индекса для сравнения
		idxKey, err := decodeMVCCKey(entry.key)
		if err != nil {
			continue
		}
		// Сравниваем ключ индекса (последний ключ блока) с искомым ключом
		if compareKeys(idxKey.Key, userKey) >= 0 {
			blockIndex = i
			break
		}
	}
	if blockIndex == -1 {
		// Ключ больше всех ключей в индексе, возможно в последнем блоке
		blockIndex = len(r.indexEntries) - 1
	}
	if blockIndex < 0 {
		return nil, false
	}

	// Читаем блок
	blockOffset := r.indexEntries[blockIndex].offset
	if _, err := r.file.Seek(int64(blockOffset), io.SeekStart); err != nil {
		return nil, false
	}
	var blockSize uint32
	if err := binary.Read(r.file, binary.LittleEndian, &blockSize); err != nil {
		return nil, false
	}
	blockData := make([]byte, blockSize)
	if _, err := io.ReadFull(r.file, blockData); err != nil {
		return nil, false
	}

	// Ищем ключ в блоке
	pos := 0
	for pos < len(blockData) {
		keyLen := binary.LittleEndian.Uint32(blockData[pos:])
		valLen := binary.LittleEndian.Uint32(blockData[pos+4:])
		entryKey := blockData[pos+8 : pos+8+int(keyLen)]
		entryVal := blockData[pos+8+int(keyLen) : pos+8+int(keyLen)+int(valLen)]
		pos += 8 + int(keyLen) + int(valLen)

		// Декодируем ключ из SSTable
		mvccKey, err := decodeMVCCKey(entryKey)
		if err != nil {
			continue
		}
		// Сравниваем ключи
		if compareKeys(mvccKey.Key, userKey) == 0 {
			// Проверяем timestamp (MVCCKey содержит timestamp)
			if mvccKey.Timestamp == key.Timestamp {
				return entryVal, true
			}
			// Если timestamp не совпадает, продолжаем искать (в SSTable хранятся разные версии)
		}
	}

	return nil, false
}

// Close закрывает файл SSTable.
func (r *Reader) Close() error {
	return r.file.Close()
}
