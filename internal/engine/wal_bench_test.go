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
	"path/filepath"
	"testing"
	"time"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

func BenchmarkWALWrite_Sync(b *testing.B) {
	benchmarkWALWrite(b, false)
}

func BenchmarkWALWrite_GroupCommit(b *testing.B) {
	benchmarkWALWrite(b, true)
}

func benchmarkWALWrite(b *testing.B, groupCommit bool) {
	dir := b.TempDir()
	path := filepath.Join(dir, "wal.log")

	opts := DefaultWALOptions()
	opts.GroupCommitEnabled = groupCommit
	if groupCommit {
		opts.GroupCommitInterval = 10 * time.Millisecond
	}

	wal, err := OpenWALWithOptions(path, opts)
	if err != nil {
		b.Fatalf("failed to open wal: %v", err)
	}
	defer errors.CloseWithFatal(wal, "wal")

	entry := &WalEntry{
		Op:        OpPut,
		Key:       []byte("test-key"),
		Value:     []byte("test-value"),
		Timestamp: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.Timestamp = uint64(i) + 1
		if err := wal.Write(entry); err != nil {
			b.Fatalf("write failed: %v", err)
		}
	}
	b.StopTimer()

	// Для группового коммита принудительно сбрасываем, чтобы все данные записались
	// (иначе benchmark может завершиться до flush и данные потеряются)
	if groupCommit {
		if err := wal.Flush(); err != nil {
			b.Fatalf("flush failed: %v", err)
		}
	}
}

func BenchmarkWALWriteParallel_Sync(b *testing.B) {
	benchmarkWALWriteParallel(b, false)
}

func BenchmarkWALWriteParallel_GroupCommit(b *testing.B) {
	benchmarkWALWriteParallel(b, true)
}

func benchmarkWALWriteParallel(b *testing.B, groupCommit bool) {
	dir := b.TempDir()
	path := filepath.Join(dir, "wal.log")

	opts := DefaultWALOptions()
	opts.GroupCommitEnabled = groupCommit
	if groupCommit {
		opts.GroupCommitInterval = 10 * time.Millisecond
	}

	wal, err := OpenWALWithOptions(path, opts)
	if err != nil {
		b.Fatalf("failed to open wal: %v", err)
	}
	defer errors.CloseWithFatal(wal, "wal")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		entry := &WalEntry{
			Op:        OpPut,
			Key:       []byte("test-key"),
			Value:     []byte("test-value"),
			Timestamp: 1,
		}
		i := 0
		for pb.Next() {
			entry.Timestamp = uint64(i) + 1
			if err := wal.Write(entry); err != nil {
				b.Fatalf("write failed: %v", err)
			}
			i++
		}
	})
	b.StopTimer()

	if groupCommit {
		if err := wal.Flush(); err != nil {
			b.Fatalf("flush failed: %v", err)
		}
	}
}

// BenchmarkWALWriteWithFlush_GroupCommit измеряет полный цикл: запись + flush.
// В отличие от BenchmarkWALWrite_GroupCommit, этот бенчмарк принудительно вызывает
// Flush() после каждой записи, что имитирует синхронный режим, но через механизм
// группового коммита. Это позволяет корректно сравнить производительность
// синхронного режима и группового коммита с учётом времени fsync.
func BenchmarkWALWriteWithFlush_GroupCommit(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "wal.log")

	opts := DefaultWALOptions()
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 100 * time.Millisecond // большой интервал, чтобы flush не срабатывал автоматически

	wal, err := OpenWALWithOptions(path, opts)
	if err != nil {
		b.Fatalf("failed to open wal: %v", err)
	}
	defer errors.CloseWithFatal(wal, "wal")

	entry := &WalEntry{
		Op:        OpPut,
		Key:       []byte("test-key"),
		Value:     []byte("test-value"),
		Timestamp: 1,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.Timestamp = uint64(i) + 1
		if err := wal.Write(entry); err != nil {
			b.Fatalf("write failed: %v", err)
		}
		// Принудительный flush для каждой записи (имитация синхронного режима)
		// В реальном Group Commit flush происходит раз в N записей
		if err := wal.Flush(); err != nil {
			b.Fatalf("flush failed: %v", err)
		}
	}
	b.StopTimer()
}

// BenchmarkWALWriteBatch_GroupCommit измеряет батчевую запись с групповым коммитом.
// Это реальный сценарий использования Group Commit: пишем батч записей,
// затем один flush на весь батч. Бенчмарк показывает, сколько времени занимает
// одна запись в среднем при групповом коммите с учётом времени fsync.
func BenchmarkWALWriteBatch_GroupCommit(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "wal.log")

	opts := DefaultWALOptions()
	opts.GroupCommitEnabled = true
	opts.GroupCommitInterval = 10 * time.Millisecond

	wal, err := OpenWALWithOptions(path, opts)
	if err != nil {
		b.Fatalf("failed to open wal: %v", err)
	}
	defer errors.CloseWithFatal(wal, "wal")

	entry := &WalEntry{
		Op:        OpPut,
		Key:       []byte("test-key"),
		Value:     []byte("test-value"),
		Timestamp: 1,
	}

	// Пишем батчами по 100 записей
	const batchSize = 100

	b.ResetTimer()
	for i := 0; i < b.N; i += batchSize {
		for j := 0; j < batchSize && i+j < b.N; j++ {
			entry.Timestamp = uint64(i+j) + 1
			if err := wal.Write(entry); err != nil {
				b.Fatalf("write failed: %v", err)
			}
		}
		// Один flush на батч
		if err := wal.Flush(); err != nil {
			b.Fatalf("flush failed: %v", err)
		}
	}
	b.StopTimer()
}
