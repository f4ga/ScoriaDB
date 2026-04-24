package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"scoriadb/internal/engine/vfs"
)

// SSTableInfo содержит метаданные одного SSTable файла.
type SSTableInfo struct {
	FileNum uint64 `json:"file_num"`
	Level   int    `json:"level"`
	MinKey  []byte `json:"min_key"`
	MaxKey  []byte `json:"max_key"`
	Size    uint64 `json:"size"`
}

// VersionEdit представляет одно атомарное изменение в составе файлов.
type VersionEdit struct {
	NewFiles    []SSTableInfo `json:"new_files,omitempty"`
	DeletedFiles []SSTableInfo `json:"deleted_files,omitempty"`
	NextFileNum uint64        `json:"next_file_num,omitempty"`
}

// Manifest управляет журналом метаданных SSTable.
type Manifest struct {
	mu       sync.Mutex
	vfs      vfs.VFS
	file     vfs.File
	filePath string
	// Текущее состояние, восстановленное после чтения манифеста.
	levels      [][]SSTableInfo
	nextFileNum uint64
}

// NewManifest создаёт новый манифест (или открывает существующий) по указанному пути.
func NewManifest(vfs vfs.VFS, path string) (*Manifest, error) {
	if err := vfs.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("failed to create manifest directory: %w", err)
	}

	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest file: %w", err)
	}

	m := &Manifest{
		vfs:      vfs,
		file:     file,
		filePath: path,
		levels:   make([][]SSTableInfo, 10), // предполагаем максимум 10 уровней
		nextFileNum: 1,
	}

	// Восстанавливаем состояние из существующего файла
	if err := m.recover(); err != nil {
		file.Close()
		return nil, fmt.Errorf("failed to recover manifest: %w", err)
	}

	return m, nil
}

// recover читает все записи из файла и применяет их для восстановления текущего состояния.
func (m *Manifest) recover() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Перемещаемся в начало файла
	if _, err := m.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("seek failed: %w", err)
	}

	decoder := json.NewDecoder(m.file)
	for {
		var edit VersionEdit
		if err := decoder.Decode(&edit); err != nil {
			if err == io.EOF {
				break
			}
			// Если JSON повреждён, останавливаемся на последней корректной записи
			// (это допустимо, потому что каждая запись завершается символом новой строки)
			break
		}
		m.applyEdit(&edit)
	}

	return nil
}

// applyEdit применяет VersionEdit к in‑memory состоянию (без записи на диск).
func (m *Manifest) applyEdit(edit *VersionEdit) {
	// Удаляем файлы
	for _, df := range edit.DeletedFiles {
		if df.Level < len(m.levels) {
			filtered := make([]SSTableInfo, 0, len(m.levels[df.Level]))
			for _, f := range m.levels[df.Level] {
				if f.FileNum != df.FileNum {
					filtered = append(filtered, f)
				}
			}
			m.levels[df.Level] = filtered
		}
	}

	// Добавляем новые файлы
	for _, nf := range edit.NewFiles {
		level := nf.Level
		if level >= len(m.levels) {
			// расширяем уровни при необходимости
			newLevels := make([][]SSTableInfo, level+1)
			copy(newLevels, m.levels)
			m.levels = newLevels
		}
		m.levels[level] = append(m.levels[level], nf)
		// Сортируем по MinKey для быстрого поиска
		sort.Slice(m.levels[level], func(i, j int) bool {
			return compareKeys(m.levels[level][i].MinKey, m.levels[level][j].MinKey) < 0
		})
	}

	// Обновляем счётчик файлов
	if edit.NextFileNum > 0 {
		m.nextFileNum = edit.NextFileNum
	}
}

// Apply записывает новую VersionEdit в манифест и применяет её к состоянию.
func (m *Manifest) Apply(edit *VersionEdit) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Сериализуем в JSON с новой строкой
	data, err := json.Marshal(edit)
	if err != nil {
		return fmt.Errorf("failed to marshal version edit: %w", err)
	}
	data = append(data, '\n')

	// Записываем в файл
	if _, err := m.file.Write(data); err != nil {
		return fmt.Errorf("failed to write manifest entry: %w", err)
	}
	// Синхронизируем, чтобы гарантировать сохранность
	if err := m.file.Sync(); err != nil {
		return fmt.Errorf("failed to sync manifest: %w", err)
	}

	// Применяем к состоянию в памяти
	m.applyEdit(edit)
	return nil
}

// GetLevels возвращает копию текущего распределения файлов по уровням.
func (m *Manifest) GetLevels() [][]SSTableInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Глубокая копия
	result := make([][]SSTableInfo, len(m.levels))
	for i, level := range m.levels {
		result[i] = make([]SSTableInfo, len(level))
		copy(result[i], level)
	}
	return result
}

// NextFileNum возвращает следующий доступный номер файла.
func (m *Manifest) NextFileNum() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.nextFileNum
}

// Close освобождает ресурсы манифеста.
func (m *Manifest) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.file.Close()
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