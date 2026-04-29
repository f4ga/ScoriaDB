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
func NewRegistry(rootDir string) (*Registry, error) {
    if err := os.MkdirAll(rootDir, 0755); err != nil {
        return nil, fmt.Errorf("failed to create root directory: %w", err)
    }

    reg := &Registry{
        rootDir: rootDir,
        cfs:     make(map[string]*engine.LSMEngine),
    }

    // Create default CF
    if err := reg.CreateCF("default"); err != nil {
        return nil, fmt.Errorf("failed to create default CF: %w", err)
    }

    // Scan existing subdirectories and load them as CFs
    entries, err := os.ReadDir(rootDir)
    if err != nil {
        return nil, fmt.Errorf("failed to read root directory: %w", err)
    }
    for _, entry := range entries {
        if entry.IsDir() && entry.Name() != "default" {
            if _, exists := reg.cfs[entry.Name()]; !exists {
                if err := reg.CreateCF(entry.Name()); err != nil {
                    fmt.Fprintf(os.Stderr, "Registry: failed to load CF %s: %v\n", entry.Name(), err)
                }
            }
        }
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