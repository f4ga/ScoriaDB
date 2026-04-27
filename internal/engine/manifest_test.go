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

package engine

import (
	"os"
	"path/filepath"
	"scoriadb/internal/engine/vfs"
	"testing"
)

func TestNewManifest(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MANIFEST")

	vfs := vfs.NewDefaultVFS()
	m, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}
	defer m.Close()

	// Проверяем начальное состояние
	levels := m.GetLevels()
	if len(levels) != 10 {
		t.Errorf("expected 10 levels, got %d", len(levels))
	}
	for _, level := range levels {
		if len(level) != 0 {
			t.Errorf("expected empty level, got %v", level)
		}
	}
	if m.NextFileNum() != 1 {
		t.Errorf("expected next file num 1, got %d", m.NextFileNum())
	}
}

func TestManifestApply(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MANIFEST")

	vfs := vfs.NewDefaultVFS()
	m, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}
	defer m.Close()

	// Добавляем файл на уровень 0
	edit := &VersionEdit{
		NewFiles: []SSTableInfo{
			{
				FileNum: 1,
				Level:   0,
				MinKey:  []byte("a"),
				MaxKey:  []byte("b"),
				Size:    1024,
			},
		},
		NextFileNum: 2,
	}
	if err := m.Apply(edit); err != nil {
		t.Fatalf("failed to apply edit: %v", err)
	}

	levels := m.GetLevels()
	if len(levels[0]) != 1 {
		t.Errorf("expected 1 file in level 0, got %d", len(levels[0]))
	}
	if levels[0][0].FileNum != 1 {
		t.Errorf("expected file num 1, got %d", levels[0][0].FileNum)
	}
	if m.NextFileNum() != 2 {
		t.Errorf("expected next file num 2, got %d", m.NextFileNum())
	}

	// Удаляем файл
	edit2 := &VersionEdit{
		DeletedFiles: []SSTableInfo{
			{
				FileNum: 1,
				Level:   0,
			},
		},
		NextFileNum: 3,
	}
	if err := m.Apply(edit2); err != nil {
		t.Fatalf("failed to apply delete edit: %v", err)
	}
	levels = m.GetLevels()
	if len(levels[0]) != 0 {
		t.Errorf("expected level 0 empty after deletion, got %d files", len(levels[0]))
	}
	if m.NextFileNum() != 3 {
		t.Errorf("expected next file num 3, got %d", m.NextFileNum())
	}
}

func TestManifestRecover(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MANIFEST")

	vfs := vfs.NewDefaultVFS()
	// Создаём манифест и применяем несколько изменений
	m1, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}

	edit1 := &VersionEdit{
		NewFiles: []SSTableInfo{
			{
				FileNum: 1,
				Level:   0,
				MinKey:  []byte("a"),
				MaxKey:  []byte("b"),
				Size:    1024,
			},
		},
		NextFileNum: 2,
	}
	if err := m1.Apply(edit1); err != nil {
		t.Fatalf("failed to apply edit1: %v", err)
	}
	edit2 := &VersionEdit{
		NewFiles: []SSTableInfo{
			{
				FileNum: 2,
				Level:   1,
				MinKey:  []byte("c"),
				MaxKey:  []byte("d"),
				Size:    2048,
			},
		},
		NextFileNum: 3,
	}
	if err := m1.Apply(edit2); err != nil {
		t.Fatalf("failed to apply edit2: %v", err)
	}
	m1.Close()

	// Открываем заново, должно восстановить состояние
	m2, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to reopen manifest: %v", err)
	}
	defer m2.Close()

	levels := m2.GetLevels()
	if len(levels[0]) != 1 || levels[0][0].FileNum != 1 {
		t.Errorf("level 0 mismatch: %v", levels[0])
	}
	if len(levels[1]) != 1 || levels[1][0].FileNum != 2 {
		t.Errorf("level 1 mismatch: %v", levels[1])
	}
	if m2.NextFileNum() != 3 {
		t.Errorf("expected next file num 3, got %d", m2.NextFileNum())
	}
}

func TestManifestGetLevels(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MANIFEST")

	vfs := vfs.NewDefaultVFS()
	m, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to create manifest: %v", err)
	}
	defer m.Close()

	// Добавляем файлы на разные уровни
	edit := &VersionEdit{
		NewFiles: []SSTableInfo{
			{FileNum: 1, Level: 0, MinKey: []byte("a"), MaxKey: []byte("b"), Size: 100},
			{FileNum: 2, Level: 1, MinKey: []byte("c"), MaxKey: []byte("d"), Size: 200},
			{FileNum: 3, Level: 1, MinKey: []byte("e"), MaxKey: []byte("f"), Size: 300},
		},
		NextFileNum: 4,
	}
	if err := m.Apply(edit); err != nil {
		t.Fatalf("failed to apply edit: %v", err)
	}

	levels := m.GetLevels()
	if len(levels) < 2 {
		t.Fatalf("expected at least 2 levels, got %d", len(levels))
	}
	if len(levels[0]) != 1 {
		t.Errorf("expected 1 file in level 0, got %d", len(levels[0]))
	}
	if len(levels[1]) != 2 {
		t.Errorf("expected 2 files in level 1, got %d", len(levels[1]))
	}
	// Проверяем сортировку по MinKey
	if string(levels[1][0].MinKey) != "c" || string(levels[1][1].MinKey) != "e" {
		t.Errorf("level 1 not sorted correctly: %v", levels[1])
	}
}

func TestManifestEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MANIFEST")

	// Создаём пустой файл
	vfs := vfs.NewDefaultVFS()
	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()

	m, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to open empty manifest: %v", err)
	}
	defer m.Close()

	// Должен быть дефолтное состояние
	if m.NextFileNum() != 1 {
		t.Errorf("expected next file num 1, got %d", m.NextFileNum())
	}
}

func TestManifestCorruptedFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "MANIFEST")

	vfs := vfs.NewDefaultVFS()
	// Пишем некорректный JSON
	file, err := vfs.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.Write([]byte("{invalid json\n"))
	file.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Манифест должен открыться (восстановление остановится на ошибке)
	m, err := NewManifest(vfs, path)
	if err != nil {
		t.Fatalf("failed to open corrupted manifest: %v", err)
	}
	defer m.Close()

	// Состояние должно быть дефолтным
	if m.NextFileNum() != 1 {
		t.Errorf("expected next file num 1, got %d", m.NextFileNum())
	}
}
