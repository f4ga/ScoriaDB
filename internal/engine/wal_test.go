package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWALWriteRecover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.log")

	wal, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("failed to open wal: %v", err)
	}
	defer wal.Close()

	// Записываем несколько операций
	entries := []*WalEntry{
		{Op: OpPut, Key: []byte("key1"), Value: []byte("value1"), Timestamp: 100},
		{Op: OpPut, Key: []byte("key2"), Value: []byte("value2"), Timestamp: 200},
		{Op: OpDelete, Key: []byte("key1"), Value: nil, Timestamp: 300},
	}
	for _, entry := range entries {
		if err := wal.Write(entry); err != nil {
			t.Fatalf("failed to write entry: %v", err)
		}
	}

	// Восстанавливаем
	var recovered []*WalEntry
	err = wal.Recover(func(entry *WalEntry) error {
		recovered = append(recovered, entry)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to recover: %v", err)
	}

	if len(recovered) != len(entries) {
		t.Fatalf("expected %d entries, got %d", len(entries), len(recovered))
	}
	for i, exp := range entries {
		got := recovered[i]
		if got.Op != exp.Op {
			t.Errorf("entry %d: op mismatch: expected %v, got %v", i, exp.Op, got.Op)
		}
		if string(got.Key) != string(exp.Key) {
			t.Errorf("entry %d: key mismatch: expected %s, got %s", i, exp.Key, got.Key)
		}
		if string(got.Value) != string(exp.Value) {
			t.Errorf("entry %d: value mismatch: expected %s, got %s", i, exp.Value, got.Value)
		}
		if got.Timestamp != exp.Timestamp {
			t.Errorf("entry %d: timestamp mismatch: expected %d, got %d", i, exp.Timestamp, got.Timestamp)
		}
	}
}

func TestWALCRCError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.log")

	wal, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("failed to open wal: %v", err)
	}
	defer wal.Close()

	// Записываем корректную запись
	entry := &WalEntry{Op: OpPut, Key: []byte("key"), Value: []byte("value"), Timestamp: 1}
	if err := wal.Write(entry); err != nil {
		t.Fatalf("failed to write: %v", err)
	}

	// Портим файл (изменяем байт в середине)
	file, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file for corruption: %v", err)
	}
	// Смещаемся на позицию после заголовка (например, 20 байт)
	file.Seek(20, 0)
	file.Write([]byte{0xFF})
	file.Close()

	// Восстановление должно вернуть ошибку CRC
	err = wal.Recover(func(entry *WalEntry) error {
		return nil
	})
	if err == nil {
		t.Error("expected CRC error, got nil")
	}
}

func TestWALEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wal.log")

	wal, err := OpenWAL(path)
	if err != nil {
		t.Fatalf("failed to open wal: %v", err)
	}
	defer wal.Close()

	// Восстановление из пустого WAL не должно вызывать ошибок
	var count int
	err = wal.Recover(func(entry *WalEntry) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 entries, got %d", count)
	}
}