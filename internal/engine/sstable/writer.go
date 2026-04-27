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
	blockBuf      []byte
	blockEntries  int
	blockStartKey []byte
	blockStartOff uint64

	// Индекс блоков
	indexEntries [][]byte
	indexOffsets []uint64

	// Фильтр Блума
	bloomFilter *BloomFilter

	// Ключи для фильтра
	keys [][]byte

	// Минимальный и максимальный ключи (пользовательские ключи)
	minKey []byte
	maxKey []byte
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

	// Обновляем min/max ключи
	if w.minKey == nil || compareKeys(key.Key, w.minKey) < 0 {
		w.minKey = key.Key
	}
	if w.maxKey == nil || compareKeys(key.Key, w.maxKey) > 0 {
		w.maxKey = key.Key
	}

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

// Finish завершает запись SSTable, записывает индекс, фильтр Блума, диапазонные ключи и футер.
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
		w.offset += 4
		if _, err := w.writer.Write(key); err != nil {
			return err
		}
		w.offset += uint64(len(key))
		// Записываем смещение блока
		if err := binary.Write(w.writer, binary.LittleEndian, w.indexOffsets[i]); err != nil {
			return err
		}
		w.offset += 8
	}
	indexSize := w.offset - indexStart

	// Записываем фильтр Блума
	bloomStart := w.offset
	bloomBytes := w.bloomFilter.Encode()
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(bloomBytes))); err != nil {
		return err
	}
	w.offset += 4
	if _, err := w.writer.Write(bloomBytes); err != nil {
		return err
	}
	w.offset += uint64(len(bloomBytes))
	bloomSize := w.offset - bloomStart

	// Записываем минимальный и максимальный ключи (диапазонный фильтр)
	minKeyStart := w.offset
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(w.minKey))); err != nil {
		return err
	}
	if _, err := w.writer.Write(w.minKey); err != nil {
		return err
	}
	// Обновляем offset после записи минимального ключа
	w.offset += 4 + uint64(len(w.minKey))

	maxKeyStart := w.offset
	if err := binary.Write(w.writer, binary.LittleEndian, uint32(len(w.maxKey))); err != nil {
		return err
	}
	if _, err := w.writer.Write(w.maxKey); err != nil {
		return err
	}
	// Обновляем offset после записи максимального ключа
	w.offset += 4 + uint64(len(w.maxKey))

	// Записываем футер
	// Вычисляем длины ключей (без префикса длины)
	minKeyLen := uint64(len(w.minKey))
	maxKeyLen := uint64(len(w.maxKey))
	footer := Footer{
		IndexOffset:  indexStart,
		IndexSize:    indexSize,
		BloomOffset:  bloomStart,
		BloomSize:    bloomSize,
		NumKeys:      uint64(len(w.keys)),
		Magic:        MagicNumber,
		MinKeyOffset: minKeyStart,
		MinKeyLength: minKeyLen,
		MaxKeyOffset: maxKeyStart,
		MaxKeyLength: maxKeyLen,
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

// compareKeys сравнивает два ключа лексикографически.
func compareKeys(a, b []byte) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}

// Footer представляет футер SSTable.
type Footer struct {
	IndexOffset  uint64
	IndexSize    uint64
	BloomOffset  uint64
	BloomSize    uint64
	NumKeys      uint64
	Magic        uint64
	MinKeyOffset uint64
	MinKeyLength uint64
	MaxKeyOffset uint64
	MaxKeyLength uint64
}
