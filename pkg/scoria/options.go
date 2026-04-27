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

package scoria

import (
	"scoriadb/internal/engine/vfs"
)

// Options содержит настройки для открытия/создания базы данных.
type Options struct {
	// WorkDir — рабочая директория, где будут храниться данные.
	WorkDir string
	// MemTableSize — максимальный размер MemTable в байтах перед flush.
	// Если 0, используется значение по умолчанию (64 MiB).
	MemTableSize int64
	// Levels — настройки уровней компакшена.
	// Если nil, используются настройки по умолчанию.
	Levels []CompactionLevelOpts
	// VFS — абстракция файловой системы.
	// Если nil, используется vfs.DefaultVFS.
	VFS vfs.VFS
}

// CompactionLevelOpts содержит настройки для одного уровня LSM-дерева.
type CompactionLevelOpts struct {
	// Level номер уровня (0, 1, …).
	Level int
	// MaxFiles максимальное количество SSTable на этом уровне.
	// Если 0, используется значение по умолчанию.
	MaxFiles int
	// TargetSize целевой размер уровня в байтах.
	// Если 0, используется значение по умолчанию.
	TargetSize int64
}

// DefaultOptions возвращает Options с настройками по умолчанию.
func DefaultOptions(workDir string) Options {
	return Options{
		WorkDir:      workDir,
		MemTableSize: 64 * 1024 * 1024, // 64 MiB
		Levels:       nil,               // пока используем настройки движка по умолчанию
		VFS:          nil,               // будет использоваться vfs.DefaultVFS внутри движка
	}
}