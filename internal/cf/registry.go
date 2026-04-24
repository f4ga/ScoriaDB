package cf

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"scoriadb/internal/engine"
)

// Registry управляет множеством Column Families.
// Каждое CF — отдельный экземпляр LSMEngine со своей директорией.
type Registry struct {
	mu      sync.RWMutex
	rootDir string
	cfs     map[string]*engine.LSMEngine // имя CF → движок
}

// NewRegistry создаёт новый реестр CF.
// rootDir — корневая директория, где будут создаваться поддиректории для каждого CF.
func NewRegistry(rootDir string) (*Registry, error) {
	// Создаём корневую директорию (если не существует)
	// Используем стандартную файловую систему, так как VFS пока не внедрён в конструктор движка.
	// TODO: передавать VFS в опциях
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	reg := &Registry{
		rootDir: rootDir,
		cfs:     make(map[string]*engine.LSMEngine),
	}

	// Создаём CF "default" по умолчанию
	if err := reg.CreateCF("default"); err != nil {
		return nil, fmt.Errorf("failed to create default CF: %w", err)
	}

	return reg, nil
}

// CreateCF создаёт новое Column Family с указанным именем.
// Если CF уже существует, возвращает ошибку.
func (r *Registry) CreateCF(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.cfs[name]; exists {
		return fmt.Errorf("CF %q already exists", name)
	}

	// Директория для CF: <rootDir>/<name>/
	cfDir := filepath.Join(r.rootDir, name)

	// Создаём движок LSMEngine
	eng, err := engine.NewLSMEngine(cfDir)
	if err != nil {
		return fmt.Errorf("failed to create LSMEngine for CF %q: %w", name, err)
	}

	r.cfs[name] = eng
	return nil
}

// GetCF возвращает движок для указанного CF.
// Если CF не существует, возвращает ошибку.
func (r *Registry) GetCF(name string) (*engine.LSMEngine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	eng, exists := r.cfs[name]
	if !exists {
		return nil, fmt.Errorf("CF %q not found", name)
	}
	return eng, nil
}

// DropCF удаляет CF и освобождает связанные ресурсы.
// Запрещено удалять системные CF (__auth__, __meta__).
func (r *Registry) DropCF(name string) error {
	if isSystemCF(name) {
		return fmt.Errorf("cannot drop system CF %q", name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	eng, exists := r.cfs[name]
	if !exists {
		return fmt.Errorf("CF %q not found", name)
	}

	// Закрываем движок
	if err := eng.Close(); err != nil {
		return fmt.Errorf("failed to close engine for CF %q: %w", name, err)
	}

	delete(r.cfs, name)
	return nil
}

// ListCFs возвращает список имён всех CF.
func (r *Registry) ListCFs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.cfs))
	for name := range r.cfs {
		names = append(names, name)
	}
	return names
}

// Close закрывает все CF и освобождает ресурсы реестра.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, eng := range r.cfs {
		if err := eng.Close(); err != nil {
			// Запоминаем первую ошибку, но продолжаем закрывать остальные
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to close CF %q: %w", name, err)
			}
		}
	}
	r.cfs = nil
	return firstErr
}

// isSystemCF возвращает true, если имя CF является системным.
func isSystemCF(name string) bool {
	return name == "__auth__" || name == "__meta__" || name == "__keyspace__"
}