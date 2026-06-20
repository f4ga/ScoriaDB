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

package errors

import (
	"fmt"
	"io"
	"os"

	"github.com/f4ga/ScoriaDB/internal/logger"
)

// RemoveAll removes a directory tree and logs a warning if an error occurs.
// Ignores os.ErrNotExist errors.
func RemoveAll(path string) {
	if err := os.RemoveAll(path); err != nil {
		logger.Warn("failed to remove %s: %v", path, err)
	}
}

// CloseWithLog closes an io.Closer and logs a warning if an error occurs.
// Used in production code where close errors are non-critical.
func CloseWithLog(closer io.Closer, name string) {
	if err := closer.Close(); err != nil {
		logger.Warn("failed to close %s: %v", name, err)
	}
}

// CloseWithFatal closes an io.Closer and calls logger.Fatal if an error occurs.
// Used in tests where close errors should fail immediately.
func CloseWithFatal(closer io.Closer, name string) {
	if err := closer.Close(); err != nil {
		logger.Fatal("failed to close %s: %v", name, err)
	}
}

// RemoveWithLog removes a file and logs a warning if an error occurs.
// Ignores os.ErrNotExist errors.
func RemoveWithLog(path string) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		logger.Warn("failed to remove %s: %v", path, err)
	}
}

// Remove removes a file and returns an error if one occurs.
// Ignores os.ErrNotExist errors.
func Remove(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// CloseWithError closes an io.Closer and returns an error if one occurs.
func CloseWithError(closer io.Closer, name string) error {
	if err := closer.Close(); err != nil {
		return fmt.Errorf("failed to close %s: %w", name, err)
	}
	return nil
}
