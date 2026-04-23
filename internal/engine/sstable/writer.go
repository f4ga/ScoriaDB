package sstable

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"os"
	"scoriadb/internal/mvcc"
)

const (
	// BlockSize размер блока данных (16 КБ)
	BlockSize = 16 * 1024
	// BloomFilterBitsPerKey количество бит на ключ для фильтра Блума (вероятность ошибки ~0.01)
	BloomFilterBitsPerKey = 10
	// MagicNumber магическое число в футере
	MagicNumber = 0x53434F5249415F53 // "SCORIA_S" в ASCII
)

// Writer записывает SSTable в файл.
type Writer struct {
	file   *os.File
	writer *bufio.Writer
	offset uint64

	// Текущий блок
	blockBuf       []byte
	blockEntries   int
	blockStartKey  []byte
	blockStartOff  uint64

	// Индекс блоков
	indexEntries [][]byte
	indexOffsets []uint64

	// Фильтр Блума
	bloomFilter *BloomFilter

	// Ключи для фильтра
	keys [][]byte
}

// NewWriter создает новый Writer для записи SSTable.
func NewWriter(path string) (*Writer, error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSTable file: %w", err)
	}
	writer := bufio.NewWriter(file)

	return &Writer{
		file:         file,
		writer:       writer,
		blockBuf:     make([]byte, 0, BlockSize),
		bloomFilter:  NewBloomFilter(BloomFilterBitsPerKey),
		indexEntries: make([][]byte, 0),
		indexOffsets: make([]uint64, 0),
		keys:         make([][]byte, 0),
	}, nil
}

// Append добавляет ключ-значение в SSTable.
func (w *Writer) Append(key mvcc.MVCCKey, value []byte) error {
	// Добавляем ключ в фильтр Блума
	w.bloomFilter.Add(key.Key)
	w.keys = append(w.keys, key.Key)

	// Сериализуем ключ и значение
	keyBytes := encodeMVCCKey(key)
	entry := encodeEntry(keyBytes, value)

	// Если текущий блок переполнится, сбрасываем его
	if len(w.blockBuf)+len(entry) > BlockSize {
		if err := w.flushBlock(); err != nil {
			return err
		}
	}

	// Запоминаем первый ключ блока
	if w.blockEntries == 0 {
		w.blockStartKey = keyBytes
		w.blockStartOff = w.offset
	}

	// Добавляем запись в блок
	w.blockBuf = append(w.blockBuf, entry...)
	w.blockEntries++

	return nil
}

// flushBlock записывает текущий блок на диск и добавляет запись в индекс.
func (w *Writer) flushBlock() error {
	if w.blockEntries == 0 {
		return nil
	}

	// Добавляем запись в индекс: последний ключ блока и смещение блока
	// Для простоты используем blockStartKey как ключ индекса
	w.indexEntries = append(w.indexEntries, w.blockStartKey)
	w.indexOffsets = append(w.indexOffsets, w.blockStartOff)

	// Записываем размер блока и данные
	blockSize := uint32(len(w.blockBuf))
	if err := binary.Write(w.writer, binary.LittleEndian, blockSize); err != nil {
		return fmt.Errorf("failed to write block size: %w", err)
	}
	if _, err := w.writer.Write(w.blockBuf); err != nil {
		return fmt.Errorf("failed to write block data: %w", err)
	}

	// Обновляем смещение
	w.offset += 4 + uint64(len(w.blockBuf))

	// Сбрасываем буфер блока
	w.blockBuf = w.blockBuf[:0]
	w.blockEntries = 0

	return nil
}

// Finish завершает запись SSTable, записывает индекс, фильтр Блума и футер.
func (w *Writer) Finish() error {
	// Сбрасываем последний блок
	if err := w.flushBlock(); err != nil {
		return err
	}

	// Записываем индекс блоков
	indexStart := w.offset
	for i, key := range w.indexEntries {
		// Записываем длину ключа и ключ
		if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(key))); err != nil {
			return err
		}
		if _, err := w.writer.Write(key); err != nil {
			return err
		}
		// Записываем смещение блока
		if err := binary.Write(w.writer, binary.LittleEndian, w.indexOffsets[i]); err != nil {
			return err
		}
	}
	indexSize := w.offset - indexStart

	// Записываем фильтр Блума
	bloomStart := w.offset
	bloomBytes := w.bloomFilter.Encode()
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(bloomBytes))); err != nil {
		return err
	}
	if _, err := w.writer.Write(bloomBytes); err != nil {
		return err
	}
	bloomSize := w.offset - bloomStart

	// Записываем футер
	footer := Footer{
		IndexOffset:   indexStart,
		IndexSize:     indexSize,
		BloomOffset:   bloomStart,
		BloomSize:     bloomSize,
		NumKeys:       uint64(len(w.keys)),
		Magic:         MagicNumber,
	}
	if err := binary.Write(w.writer, binary.LittleEndian, footer); err != nil {
		return fmt.Errorf("failed to write footer: %w", err)
	}

	// Сбрасываем буфер в файл
	if err := w.writer.Flush(); err != nil {
		return fmt.Errorf("failed to flush writer: %w", err)
	}
	return w.file.Close()
}

// encodeEntry кодирует пару ключ-значение в байты.
func encodeEntry(key, value []byte) []byte {
	kl := len(key)
	vl := len(value)
	buf := make([]byte, 4+4+kl+vl)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(kl))
	binary.LittleEndian.PutUint32(buf[4:8], uint32(vl))
	copy(buf[8:8+kl], key)
	copy(buf[8+kl:], value)
	return buf
}

// Footer представляет футер SSTable.
type Footer struct {
	IndexOffset uint64
	IndexSize   uint64
	BloomOffset uint64
	BloomSize   uint64
	NumKeys     uint64
	Magic       uint64
}