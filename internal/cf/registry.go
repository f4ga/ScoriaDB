package cf

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"scoriadb/internal/engine"
)

// Registry manages multiple Column Families.
// Each CF is a separate LSMEngine instance with its own directory.
type Registry struct {
	mu      sync.RWMutex
	rootDir string
	cfs     map[string]*engine.LSMEngine // CF name → engine
}

// NewRegistry creates a new CF registry.
// rootDir is the root directory where subdirectories for each CF will be created.
func NewRegistry(rootDir string) (*Registry, error) {
	// Create root directory (if it doesn't exist)
	// Using standard filesystem because VFS is not yet integrated into engine constructor.
	// Tracked for Release 2: pass VFS via options.
	if err := os.MkdirAll(rootDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create root directory: %w", err)
	}

	reg := &Registry{
		rootDir: rootDir,
		cfs:     make(map[string]*engine.LSMEngine),
	}

	// Create default CF "default"
	if err := reg.CreateCF("default"); err != nil {
		return nil, fmt.Errorf("failed to create default CF: %w", err)
	}

	return reg, nil
}

// CreateCF creates a new Column Family with the given name.
// If the CF already exists, returns an error.
func (r *Registry) CreateCF(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.cfs[name]; exists {
		return fmt.Errorf("CF %q already exists", name)
	}

	// Directory for CF: <rootDir>/<name>/
	cfDir := filepath.Join(r.rootDir, name)

	// Create LSMEngine
	eng, err := engine.NewLSMEngine(cfDir)
	if err != nil {
		return fmt.Errorf("failed to create LSMEngine for CF %q: %w", name, err)
	}

	r.cfs[name] = eng
	return nil
}

// GetCF returns the engine for the specified CF.
// If the CF does not exist, returns an error.
func (r *Registry) GetCF(name string) (*engine.LSMEngine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	eng, exists := r.cfs[name]
	if !exists {
		return nil, fmt.Errorf("CF %q not found", name)
	}
	return eng, nil
}

// DropCF deletes a CF and releases its associated resources.
// Dropping system CFs (__auth__, __meta__) is prohibited.
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

	// Close the engine
	if err := eng.Close(); err != nil {
		return fmt.Errorf("failed to close engine for CF %q: %w", name, err)
	}

	delete(r.cfs, name)
	return nil
}

// ListCFs returns a list of all CF names.
func (r *Registry) ListCFs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.cfs))
	for name := range r.cfs {
		names = append(names, name)
	}
	return names
}

// Close closes all CFs and releases registry resources.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var firstErr error
	for name, eng := range r.cfs {
		if err := eng.Close(); err != nil {
			// Remember the first error but continue closing others
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to close CF %q: %w", name, err)
			}
		}
	}
	r.cfs = nil
	return firstErr
}

// isSystemCF returns true if the CF name is a system CF.
func isSystemCF(name string) bool {
	return name == "__auth__" || name == "__meta__" || name == "__keyspace__"
}