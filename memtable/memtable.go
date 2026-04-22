package memtable

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// MemTable представляет in-memory хранилище ключ-значение с WAL
type MemTable struct {
	mu   sync.RWMutex
	data map[string]string
	wal  *os.File // файл Write-Ahead Log
}

// NewMemTable создает новую MemTable с опциональным восстановлением из WAL
func NewMemTable(walPath string) (*MemTable, error) {
	mt := &MemTable{
		data: make(map[string]string),
	}

	// Открываем или создаем WAL файл
	walFile, err := os.OpenFile(walPath, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}
	mt.wal = walFile

	// Восстанавливаем данные из WAL
	if err := mt.recoverFromWAL(); err != nil {
		// Закрываем файл в случае ошибки
		mt.wal.Close()
		return nil, fmt.Errorf("failed to recover from WAL: %w", err)
	}

	return mt, nil
}

// Set устанавливает значение по ключу и записывает операцию в WAL
func (mt *MemTable) Set(key, value string) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Записываем операцию в WAL
	walEntry := fmt.Sprintf("SET %s %s\n", key, value)
	if _, err := mt.wal.WriteString(walEntry); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}
	
	// Синхронизируем на диск для гарантии durability
	if err := mt.wal.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	// Обновляем данные в памяти
	mt.data[key] = value
	return nil
}

// Get возвращает значение по ключу
func (mt *MemTable) Get(key string) (string, bool) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	
	value, exists := mt.data[key]
	return value, exists
}

// Delete удаляет ключ (опционально, для полноты)
func (mt *MemTable) Delete(key string) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Записываем операцию в WAL
	walEntry := fmt.Sprintf("DEL %s\n", key)
	if _, err := mt.wal.WriteString(walEntry); err != nil {
		return fmt.Errorf("failed to write to WAL: %w", err)
	}
	if err := mt.wal.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	delete(mt.data, key)
	return nil
}

// recoverFromWAL восстанавливает данные из WAL файла
func (mt *MemTable) recoverFromWAL() error {
	// Перемещаем указатель чтения в начало файла
	if _, err := mt.wal.Seek(0, 0); err != nil {
		return fmt.Errorf("failed to seek WAL: %w", err)
	}

	scanner := bufio.NewScanner(mt.wal)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 3)
		if len(parts) < 2 {
			continue // Пропускаем некорректные строки
		}

		command := parts[0]
		key := parts[1]

		switch command {
		case "SET":
			if len(parts) != 3 {
				continue
			}
			value := parts[2]
			mt.data[key] = value
		case "DEL":
			delete(mt.data, key)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading WAL: %w", err)
	}

	return nil
}

// Close закрывает WAL файл
func (mt *MemTable) Close() error {
	if mt.wal != nil {
		return mt.wal.Close()
	}
	return nil
}

// Size возвращает количество элементов в MemTable
func (mt *MemTable) Size() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return len(mt.data)
}