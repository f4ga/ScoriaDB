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