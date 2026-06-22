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
	"io"
	"os"
	"sync"

	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

// sync.Pool для переиспользования WalEntry.
var walEntryPool = sync.Pool{
	New: func() interface{} { return &WalEntry{} },
}

func newWalEntry() *WalEntry {
	entry, ok := walEntryPool.Get().(*WalEntry)
	if !ok {
		return &WalEntry{}
	}
	return entry
}

func putWalEntry(entry *WalEntry) {
	entry.Key = nil
	entry.Value = nil
	entry.Timestamp = 0
	entry.Op = 0
	walEntryPool.Put(entry)
}

// sync.Pool для переиспользования буферов кодирования WAL записей.
// Каждый буфер — это []byte, который переиспользуется между вызовами encodeWalEntry,
// что снижает количество аллокаций в горячем пути записи.
var encodeBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 0, 256)
		return &buf
	},
}

// getEncodeBuffer возвращает буфер из пула с минимальной ёмкостью size.
// Буфер автоматически расширяется при необходимости.
func getEncodeBuffer(size int) []byte {
	ptr := encodeBufferPool.Get().(*[]byte)
	buf := *ptr
	if cap(buf) < size {
		buf = make([]byte, size)
	}
	return buf[:size]
}

// putEncodeBuffer возвращает буфер в пул для переиспользования.
// Буфер обнуляется (затирается), чтобы избежать утечки данных между записями.
func putEncodeBuffer(buf []byte) {
	// Затираем буфер, чтобы избежать утечки конфиденциальных данных
	for i := range buf {
		buf[i] = 0
	}
	ptr := &buf
	encodeBufferPool.Put(ptr)
}

// OpType тип операции в WAL.
type OpType byte

const (
	OpPut    OpType = 1
	OpDelete OpType = 2
	OpBatch  OpType = 3 // атомарный батч операций
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
	opts   WALOptions
	group  *groupCommitWriter // nil если group commit отключён
}

// OpenWAL открывает или создает WAL файл с настройками по умолчанию.
func OpenWAL(path string) (*WAL, error) {
	return OpenWALWithOptions(path, DefaultWALOptions())
}

// OpenWALWithOptions открывает или создает WAL файл с указанными настройками.
// При включённом групповом коммите (GroupCommitEnabled = true) записи буферизуются
// и периодически сбрасываются на диск с интервалом GroupCommitInterval.
// Это значительно повышает пропускную способность, но снижает durability:
// записи, сделанные после последнего сброса, могут быть потеряны при краше процесса.
// Для критичных к durability workload'ов оставьте GroupCommitEnabled = false
// (режим по умолчанию), где каждая запись немедленно синхронизируется с диском.
func OpenWALWithOptions(path string, opts WALOptions) (*WAL, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open wal file: %w", err)
	}

	// Получаем текущий размер файла
	stat, err := file.Stat()
	if err != nil {
		errors.CloseWithLog(file, "wal-file")
		return nil, fmt.Errorf("failed to stat wal file: %w", err)
	}

	wal := &WAL{
		file:   file,
		offset: stat.Size(),
		opts:   opts,
	}

	// Инициализируем групповой коммит, если включён
	if opts.GroupCommitEnabled {
		wal.group = newGroupCommitWriter(file, opts.GroupCommitInterval)
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
	// Буфер из пула — возвращаем после использования.
	// groupCommitWriter.Write() и file.Write() копируют данные внутрь,
	// поэтому buf можно безопасно вернуть в пул сразу после записи.
	defer putEncodeBuffer(buf)

	// Если включён групповой коммит, пишем в буфер
	if w.group != nil {
		err = w.group.Write(buf)
		if err != nil {
			return fmt.Errorf("failed to write to group commit buffer: %w", err)
		}
		// Обновляем offset на основе размера данных (даже если они ещё не на диске)
		w.offset += int64(len(buf))
		return nil
	}

	// Синхронный режим: пишем и синхронизируем сразу
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

// Flush принудительно сбрасывает буферизованные данные на диск.
// Если групповой коммит отключён, метод ничего не делает (данные уже на диске).
func (w *WAL) Flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.group != nil {
		return w.group.Flush()
	}
	return nil
}

// Recover читает все записи из WAL и вызывает callback для каждой.
// При обнаружении повреждённой записи (CRC mismatch, unexpected EOF) пропускает
// оставшуюся часть файла и возвращает nil — частичное восстановление считается
// успешным. Это гарантирует, что даже при повреждении WAL база данных сможет
// загрузиться с максимально возможным количеством данных.
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
			// Corrupted entry: log a warning and stop recovery.
			// This allows the database to start with the data recovered so far.
			logger.Warn("wal: corrupted entry during recovery, stopping: %v", err)
			break
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

	// Закрываем групповой коммит, если он был инициализирован
	if w.group != nil {
		if err := w.group.Close(); err != nil {
			// Пытаемся закрыть файл даже при ошибке в group.Close()
			errors.CloseWithLog(w.file, "wal-file")
			return fmt.Errorf("failed to close group commit writer: %w", err)
		}
	}

	return w.file.Close()
}

// encodeWalEntry сериализует запись в байты с CRC.
// Возвращает []byte, который НЕЛЬЗЯ изменять после возврата,
// так как буфер может быть переиспользован в следующих вызовах.
func encodeWalEntry(entry *WalEntry) ([]byte, error) {
	// Размеры
	keyLen := len(entry.Key)
	valLen := len(entry.Value)
	// Общий размер: тип (1) + timestamp (8) + keyLen (2) + valLen (4) + ключ + значение
	totalSize := 1 + 8 + 2 + 4 + keyLen + valLen

	// Берём буфер из пула
	buf := getEncodeBuffer(totalSize + 4) // +4 для CRC
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

	entry := newWalEntry()
	entry.Op = op
	entry.Key = key
	entry.Value = value
	entry.Timestamp = timestamp
	return entry, nil
}
