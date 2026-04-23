package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"scoriadb/internal/engine/sstable"
	"scoriadb/internal/mvcc"
)

const (
	// MaxMemTableSize максимальный размер MemTable в байтах перед flush
	MaxMemTableSize = 4 * 1024 * 1024 // 4 МБ
	// MaxLevel0Files максимальное количество файлов в Level0 перед compaction
	MaxLevel0Files = 4
)

// flushMemTable сбрасывает текущую MemTable в SSTable Level0.
func (e *LSMEngine) flushMemTable() error {
	// Заглушка: просто создаем пустой SSTable
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("level0_%d.sst", time.Now().UnixNano()))
	writer, err := sstable.NewWriter(sstPath)
	if err != nil {
		return fmt.Errorf("failed to create SSTable writer: %w", err)
	}
	// Записываем заглушку ключа
	key := mvcc.NewMVCCKey([]byte("__dummy__"), 1)
	if err := writer.Append(key, []byte("dummy")); err != nil {
		writer = nil
		os.Remove(sstPath)
		return fmt.Errorf("failed to append dummy key: %w", err)
	}
	if err := writer.Finish(); err != nil {
		os.Remove(sstPath)
		return fmt.Errorf("failed to finish SSTable: %w", err)
	}
	reader, err := sstable.Open(sstPath)
	if err != nil {
		os.Remove(sstPath)
		return fmt.Errorf("failed to open SSTable: %w", err)
	}
	e.mu.Lock()
	e.levels[0] = append(e.levels[0], reader)
	e.mu.Unlock()
	return nil
}

// maybeCompactLevel0 проверяет, нужно ли выполнить compaction Level0 -> Level1.
func (e *LSMEngine) maybeCompactLevel0() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.levels[0]) <= MaxLevel0Files {
		return
	}
	// Заглушка: просто удаляем все файлы Level0
	for _, reader := range e.levels[0] {
		reader.Close()
		// Удаление файла опустим
	}
	e.levels[0] = nil
}

// maybeFlush проверяет, не превысил ли MemTable лимит, и запускает flush.
func (e *LSMEngine) maybeFlush() {
	if e.memSize >= MaxMemTableSize {
		go e.flushMemTable()
	}
}