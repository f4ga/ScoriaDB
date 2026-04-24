package engine

import (
	"bytes"
	"fmt"
	"path/filepath"
	"scoriadb/internal/engine/sstable"
	// "scoriadb/internal/mvcc"  реализовать обязательно либо подумать над тем где сделать 
)

const (
	// MaxMemTableSize максимальный размер MemTable в байтах перед flush
	MaxMemTableSize = 4 * 1024 * 1024 // 4 МБ
	// MaxLevel0Files максимальное количество файлов в Level0 перед compaction
	MaxLevel0Files = 4
)

// flushMemTable сбрасывает текущую MemTable в SSTable Level0.
func (e *LSMEngine) flushMemTable() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Получаем следующий номер файла из манифеста
	fileNum := e.manifest.NextFileNum()
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Создаём writer (пока используем старый API, который работает с os)
	writer, err := sstable.NewWriter(sstPath)
	if err != nil {
		return fmt.Errorf("failed to create SSTable writer: %w", err)
	}

	// Итерируем по всем записям MemTable
	iter := e.memTable.NewIterator()
	var minKey, maxKey []byte
	var first = true
	for iter.Next() {
		key, value := iter.Key(), iter.Value()
		// Для range filter нам нужны user keys (без timestamp)
		userKey := key.Key
		if first {
			minKey = make([]byte, len(userKey))
			copy(minKey, userKey)
			maxKey = make([]byte, len(userKey))
			copy(maxKey, userKey)
			first = false
		} else {
			if bytes.Compare(userKey, minKey) < 0 {
				minKey = userKey
			}
			if bytes.Compare(userKey, maxKey) > 0 {
				maxKey = userKey
			}
		}
		if err := writer.Append(key, value); err != nil {
			writer = nil
			// Удаляем частично записанный файл через VFS
			e.vfs.Remove(sstPath)
			return fmt.Errorf("failed to append key to SSTable: %w", err)
		}
	}

	if err := writer.Finish(); err != nil {
		e.vfs.Remove(sstPath)
		return fmt.Errorf("failed to finish SSTable: %w", err)
	}

	// Открываем созданный SSTable для чтения
	reader, err := sstable.Open(sstPath)
	if err != nil {
		e.vfs.Remove(sstPath)
		return fmt.Errorf("failed to open SSTable: %w", err)
	}

	// Получаем размер файла
	stat, err := e.vfs.Stat(sstPath)
	if err != nil {
		reader.Close()
		e.vfs.Remove(sstPath)
		return fmt.Errorf("failed to stat SSTable: %w", err)
	}

	// Создаём VersionEdit для добавления нового файла
	edit := &VersionEdit{
		NewFiles: []SSTableInfo{
			{
				FileNum: fileNum,
				Level:   0,
				MinKey:  minKey,
				MaxKey:  maxKey,
				Size:    uint64(stat.Size()),
			},
		},
		NextFileNum: fileNum + 1,
	}

	// Применяем edit к манифесту
	if err := e.manifest.Apply(edit); err != nil {
		reader.Close()
		e.vfs.Remove(sstPath)
		return fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	// Добавляем reader в уровень 0
	e.levels[0] = append(e.levels[0], reader)

	// Сбрасываем MemTable (в реальности нужно создать новую пустую MemTable)
	// Пока оставим как есть - очистка MemTable будет выполнена после успешного flush
	// e.memTable = NewMemTable()
	// e.memSize = 0

	return nil
}

// maybeCompactLevel0 проверяет, нужно ли выполнить compaction Level0 -> Level1.
// Вызывает maybeCompact, который уже содержит логику проверки и запуска compaction.
func (e *LSMEngine) maybeCompactLevel0() {
	e.maybeCompact()
}

// maybeFlush проверяет, не превысил ли MemTable лимит, и запускает flush.
func (e *LSMEngine) maybeFlush() {
	if e.memSize >= MaxMemTableSize {
		go e.flushMemTable()
	}
}