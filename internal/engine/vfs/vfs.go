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
	"io"
	"os"
	"path/filepath"
)

// VFS определяет абстрактный интерфейс файловой системы.
type VFS interface {
	// OpenFile открывает файл с указанными флагами и правами доступа.
	OpenFile(name string, flag int, perm os.FileMode) (File, error)
	// Create создаёт файл, усекая его, если он существует.
	Create(name string) (File, error)
	// Open открывает файл только для чтения.
	Open(name string) (File, error)
	// Remove удаляет файл или пустую директорию.
	Remove(name string) error
	// Rename переименовывает (перемещает) файл.
	Rename(oldpath, newpath string) error
	// MkdirAll создаёт директорию, включая все родительские, если их нет.
	MkdirAll(path string, perm os.FileMode) error
	// Stat возвращает информацию о файле.
	Stat(name string) (os.FileInfo, error)
	// ReadDir читает содержимое директории.
	ReadDir(dirname string) ([]os.DirEntry, error)
}

// File представляет открытый файл.
type File interface {
	io.Reader
	io.Writer
	io.Closer
	io.Seeker
	// Sync синхронизирует содержимое файла с диском.
	Sync() error
	// Stat возвращает информацию о файле.
	Stat() (os.FileInfo, error)
	// Readdir читает содержимое директории (для директорий).
	Readdir(n int) ([]os.FileInfo, error)
}

// DefaultVFS реализует VFS, делегируя вызовы стандартным функциям пакета os.
type DefaultVFS struct{}

var _ VFS = (*DefaultVFS)(nil)

func (DefaultVFS) OpenFile(name string, flag int, perm os.FileMode) (File, error) {
	return os.OpenFile(name, flag, perm)
}

func (DefaultVFS) Create(name string) (File, error) {
	return os.Create(name)
}

func (DefaultVFS) Open(name string) (File, error) {
	return os.Open(name)
}

func (DefaultVFS) Remove(name string) error {
	return os.Remove(name)
}

func (DefaultVFS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

func (DefaultVFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (DefaultVFS) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (DefaultVFS) ReadDir(dirname string) ([]os.DirEntry, error) {
	return os.ReadDir(dirname)
}

// MockVFS можно использовать в тестах для подмены файловых операций.
// Реализация опущена для краткости, но может быть добавлена позже.
type MockVFS struct {
	// ... поля для хранения состояния
}

// Ensure DefaultVFS используется по умолчанию.
var Default = DefaultVFS{}

// NewDefaultVFS возвращает новый экземпляр DefaultVFS, реализующий интерфейс VFS.
func NewDefaultVFS() VFS {
	return DefaultVFS{}
}

// Helper функции для удобства.

// ReadFile читает весь файл в память.
func ReadFile(vfs VFS, name string) ([]byte, error) {
	f, err := vfs.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

// WriteFile записывает данные в файл, создавая его при необходимости.
func WriteFile(vfs VFS, name string, data []byte, perm os.FileMode) error {
	f, err := vfs.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	if err != nil {
		return err
	}
	return f.Sync()
}

// Exists проверяет, существует ли файл или директория.
func Exists(vfs VFS, name string) bool {
	_, err := vfs.Stat(name)
	return err == nil
}

// WalkDir обходит дерево директорий, вызывая walkFn для каждого элемента.
func WalkDir(vfs VFS, root string, walkFn filepath.WalkFunc) error {
	info, err := vfs.Stat(root)
	if err != nil {
		return walkFn(root, nil, err)
	}
	return walkDir(vfs, root, info, walkFn)
}

func walkDir(vfs VFS, path string, info os.FileInfo, walkFn filepath.WalkFunc) error {
	err := walkFn(path, info, nil)
	if err != nil {
		if info.IsDir() && err == filepath.SkipDir {
			return nil
		}
		return err
	}

	if !info.IsDir() {
		return nil
	}

	entries, err := vfs.ReadDir(path)
	if err != nil {
		return walkFn(path, info, err)
	}

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		entryInfo, err := entry.Info()
		if err != nil {
			if err := walkFn(fullPath, entryInfo, err); err != nil && err != filepath.SkipDir {
				return err
			}
		} else {
			if err := walkDir(vfs, fullPath, entryInfo, walkFn); err != nil {
				if err != filepath.SkipDir {
					return err
				}
			}
		}
	}
	return nil
}