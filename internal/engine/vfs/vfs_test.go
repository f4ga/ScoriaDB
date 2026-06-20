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

package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultVFS_OpenFile(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	f, err := vfs.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := f.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultVFS_Create(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "create.txt")

	f, err := vfs.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	// File should exist
	if _, err := vfs.Stat(path); err != nil {
		t.Errorf("file should exist after Create: %v", err)
	}
}

func TestDefaultVFS_Open(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "open.txt")

	// Create file first
	f, _ := vfs.Create(path)
	f.Write([]byte("data"))
	f.Close()

	// Open for reading
	f2, err := vfs.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f2.Close()

	buf := make([]byte, 4)
	n, err := f2.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 || string(buf) != "data" {
		t.Errorf("expected 'data', got %q", string(buf[:n]))
	}
}

func TestDefaultVFS_Remove(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "remove.txt")

	f, _ := vfs.Create(path)
	f.Close()

	if err := vfs.Remove(path); err != nil {
		t.Fatal(err)
	}

	if Exists(vfs, path) {
		t.Error("file should not exist after Remove")
	}
}

func TestDefaultVFS_Rename(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.txt")
	newPath := filepath.Join(dir, "new.txt")

	f, _ := vfs.Create(oldPath)
	f.Write([]byte("rename"))
	f.Close()

	if err := vfs.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}

	if Exists(vfs, oldPath) {
		t.Error("old file should not exist after Rename")
	}
	if !Exists(vfs, newPath) {
		t.Error("new file should exist after Rename")
	}
}

func TestDefaultVFS_MkdirAll(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	subDir := filepath.Join(dir, "a", "b", "c")

	if err := vfs.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}

	if !Exists(vfs, subDir) {
		t.Error("directory should exist after MkdirAll")
	}
}

func TestDefaultVFS_Stat(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "stat.txt")

	f, _ := vfs.Create(path)
	f.Close()

	info, err := vfs.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() {
		t.Error("file should not be a directory")
	}
	if info.Name() != "stat.txt" {
		t.Errorf("expected 'stat.txt', got %q", info.Name())
	}
}

func TestDefaultVFS_ReadDir(t *testing.T) {
	vfs := Default
	dir := t.TempDir()

	f1, _ := vfs.Create(filepath.Join(dir, "a.txt"))
	f1.Close()
	f2, _ := vfs.Create(filepath.Join(dir, "b.txt"))
	f2.Close()

	entries, err := vfs.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(entries))
	}
}

func TestReadFile(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "readfile.txt")

	data := []byte("hello world")
	if err := WriteFile(vfs, path, data, 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile(vfs, path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestWriteFile(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "writefile.txt")

	data := []byte("test data")
	if err := WriteFile(vfs, path, data, 0644); err != nil {
		t.Fatal(err)
	}

	// Verify content
	got, err := ReadFile(vfs, path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Errorf("expected %q, got %q", data, got)
	}
}

func TestExists(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	path := filepath.Join(dir, "exists.txt")

	if Exists(vfs, path) {
		t.Error("file should not exist initially")
	}

	f, _ := vfs.Create(path)
	f.Close()

	if !Exists(vfs, path) {
		t.Error("file should exist after creation")
	}
}

func TestWalkDir(t *testing.T) {
	vfs := Default
	dir := t.TempDir()

	// Create structure: dir/a.txt, dir/sub/b.txt
	if err := vfs.MkdirAll(filepath.Join(dir, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	f1, _ := vfs.Create(filepath.Join(dir, "a.txt"))
	f1.Close()
	f2, _ := vfs.Create(filepath.Join(dir, "sub", "b.txt"))
	f2.Close()

	var paths []string
	err := WalkDir(vfs, dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// WalkDir includes the root directory as the first entry
	if len(paths) != 4 {
		t.Errorf("expected 4 paths (root + 3 entries), got %d: %v", len(paths), paths)
	}
}

func TestWalkDir_NonExistentRoot(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	nonExistent := filepath.Join(dir, "nonexistent")

	err := WalkDir(vfs, nonExistent, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		t.Error("expected error for non-existent root")
	}
}

func TestWalkDir_SkipDir(t *testing.T) {
	vfs := Default
	dir := t.TempDir()

	if err := vfs.MkdirAll(filepath.Join(dir, "skip"), 0755); err != nil {
		t.Fatal(err)
	}
	f1, _ := vfs.Create(filepath.Join(dir, "skip", "a.txt"))
	f1.Close()
	f2, _ := vfs.Create(filepath.Join(dir, "keep.txt"))
	f2.Close()

	var paths []string
	err := WalkDir(vfs, dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && info.Name() == "skip" {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(dir, path)
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// WalkDir includes the root directory as the first entry
	if len(paths) != 2 || paths[1] != "keep.txt" {
		t.Errorf("expected ['.', 'keep.txt'], got %v", paths)
	}
}

func TestNewDefaultVFS(t *testing.T) {
	vfs := NewDefaultVFS()
	if vfs == nil {
		t.Fatal("NewDefaultVFS should not return nil")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "newvfs.txt")

	f, err := vfs.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

func TestDefaultVFS_OpenNonExistent(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	_, err := vfs.Open(filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Error("expected error opening non-existent file")
	}
}

func TestDefaultVFS_RemoveNonExistent(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	err := vfs.Remove(filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Error("expected error removing non-existent file")
	}
}

func TestReadFile_NonExistent(t *testing.T) {
	vfs := Default
	dir := t.TempDir()
	_, err := ReadFile(vfs, filepath.Join(dir, "nonexistent.txt"))
	if err == nil {
		t.Error("expected error reading non-existent file")
	}
}
