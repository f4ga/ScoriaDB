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
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// testCloser implements io.Closer for testing.
type testCloser struct {
	closeErr error
	closed   bool
}

func (tc *testCloser) Close() error {
	tc.closed = true
	return tc.closeErr
}

func TestCloseWithLog(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &testCloser{}
		CloseWithLog(c, "test-closer")
		if !c.closed {
			t.Error("closer should have been closed")
		}
	})

	t.Run("error", func(t *testing.T) {
		c := &testCloser{closeErr: errors.New("close error")}
		// Should not panic on error, just log
		CloseWithLog(c, "test-closer")
		if !c.closed {
			t.Error("closer should have been closed even on error")
		}
	})
}

func TestCloseWithFatal(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &testCloser{}
		CloseWithFatal(c, "test-closer")
		if !c.closed {
			t.Error("closer should have been closed")
		}
	})
}

func TestCloseWithError(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := &testCloser{}
		err := CloseWithError(c, "test-closer")
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
		if !c.closed {
			t.Error("closer should have been closed")
		}
	})

	t.Run("error", func(t *testing.T) {
		c := &testCloser{closeErr: errors.New("close error")}
		err := CloseWithError(c, "test-closer")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !c.closed {
			t.Error("closer should have been closed even on error")
		}
	})
}

func TestRemove(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.txt")
		f, _ := os.Create(path)
		f.Close()

		err := Remove(path)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("non-existent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nonexistent.txt")

		err := Remove(path)
		if err != nil {
			t.Errorf("expected nil error for non-existent file, got %v", err)
		}
	})
}

func TestRemoveWithLog(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.txt")
		f, _ := os.Create(path)
		f.Close()

		// Should not panic
		RemoveWithLog(path)
	})

	t.Run("non-existent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nonexistent.txt")

		// Should not panic for non-existent file
		RemoveWithLog(path)
	})
}

func TestRemoveAll_Error(t *testing.T) {
	// Create a directory and make it read-only to trigger an error on RemoveAll
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0444); err != nil {
		t.Skipf("cannot create read-only directory: %v", err)
	}

	// RemoveAll should log a warning but not panic
	RemoveAll(subDir)
}

func TestRemove_Error(t *testing.T) {
	// Create a directory with a file inside to trigger an error on os.Remove
	// (os.Remove on a non-empty directory should fail)
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	// Create a file inside the directory to make it non-empty
	f, err := os.Create(filepath.Join(subDir, "test.txt"))
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	f.Close()

	// Remove on a non-empty directory should fail
	err = Remove(subDir)
	if err != nil {
		// Expected: removing a non-empty directory returns an error
		t.Logf("Remove on non-empty directory returned expected error: %v", err)
	}
}

func TestRemoveWithLog_Error(t *testing.T) {
	// Create a directory with a file inside to trigger an error on os.Remove
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}
	// Create a file inside the directory to make it non-empty
	f, err := os.Create(filepath.Join(subDir, "test.txt"))
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	f.Close()

	// RemoveWithLog on a non-empty directory should log a warning but not panic
	RemoveWithLog(subDir)
}

func TestRemoveAll(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		subDir := filepath.Join(dir, "sub")
		os.MkdirAll(subDir, 0755)
		f, _ := os.Create(filepath.Join(subDir, "test.txt"))
		f.Close()

		// Should not panic
		RemoveAll(subDir)
	})

	t.Run("non-existent", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nonexistent")

		// Should not panic for non-existent path
		RemoveAll(path)
	})
}
