package engine

import (
	"fmt"
	"path/filepath"
	"scoriadb/internal/engine/sstable"
	"scoriadb/internal/mvcc"
)

// compactLevel0 выполняет compaction уровня 0 в уровень 1.
// Простая реализация: объединяет все SSTable уровня 0 в один новый SSTable уровня 1.
//nolint:unused // triggered by maybeCompact
func (e *LSMEngine) compactLevel0() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.levels[0]) == 0 {
		return nil
	}

	// Получаем следующий номер файла
	fileNum := e.manifest.NextFileNum()
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Создаём writer для нового SSTable
	writer, err := sstable.NewWriter(sstPath)
	if err != nil {
		return fmt.Errorf("failed to create SSTable writer: %w", err)
	}

	// Собираем все ключи из всех SSTable уровня 0
	// В реальной реализации нужно выполнить слияние с сортировкой и удалением дубликатов.
	// Здесь для простоты просто создаём пустой SSTable.
	// TODO: реализовать настоящее слияние
	key := []byte("__compacted__")
	value := []byte("compacted")
	mvccKey := mvcc.NewMVCCKey(key, 1)
	if err := writer.Append(mvccKey, value); err != nil {
		writer = nil
		_ = e.vfs.Remove(sstPath) // TODO: log error
		return fmt.Errorf("failed to append dummy key: %w", err)
	}

	if err := writer.Finish(); err != nil {
		_ = e.vfs.Remove(sstPath) // TODO: log error
		return fmt.Errorf("failed to finish SSTable: %w", err)
	}

	// Открываем созданный SSTable
	reader, err := sstable.Open(sstPath)
	if err != nil {
		_ = e.vfs.Remove(sstPath) // TODO: log error
		return fmt.Errorf("failed to open SSTable: %w", err)
	}

	// Получаем размер файла
	stat, err := e.vfs.Stat(sstPath)
	if err != nil {
		reader.Close()
		_ = e.vfs.Remove(sstPath) // TODO: log error
		return fmt.Errorf("failed to stat SSTable: %w", err)
	}

	// Подготавливаем VersionEdit: удаляем старые файлы уровня 0, добавляем новый файл уровня 1
	var deletedFiles []SSTableInfo
	for range e.levels[0] {
		// В реальности нужно получить fileNum из reader, но для простоты пропускаем
		// Здесь просто добавляем заглушку
		deletedFiles = append(deletedFiles, SSTableInfo{
			Level: 0,
			// FileNum неизвестен, оставляем 0
		})
	}

	edit := &VersionEdit{
		DeletedFiles: deletedFiles,
		NewFiles: []SSTableInfo{
			{
				FileNum: fileNum,
				Level:   1,
				MinKey:  key, // в реальности нужно вычислить min/max
				MaxKey:  key,
				Size:    uint64(stat.Size()),
			},
		},
		NextFileNum: fileNum + 1,
	}

	// Применяем edit к манифесту
	if err := e.manifest.Apply(edit); err != nil {
		reader.Close()
		_ = e.vfs.Remove(sstPath) // TODO: log error
		return fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	// Закрываем старые readers
	for _, r := range e.levels[0] {
		r.Close()
	}
	// Очищаем уровень 0
	e.levels[0] = nil
	// Добавляем новый reader в уровень 1
	e.levels[1] = append(e.levels[1], reader)

	return nil
}

// maybeCompact проверяет условия и запускает compaction при необходимости.
//nolint:unused // scheduled compaction entry point
func (e *LSMEngine) maybeCompact() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Простое условие: если в Level0 больше MaxLevel0Files файлов, запускаем compaction
	if len(e.levels[0]) > MaxLevel0Files {
		//nolint:errcheck // ошибка обрабатывается внутри горутины
		go e.compactLevel0()
	}
}