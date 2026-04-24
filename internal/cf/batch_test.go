package cf

import (
	"testing"
)

func TestCFWriteBatchBasic(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	// Создаём два CF
	err = reg.CreateCF("cf1")
	if err != nil {
		t.Fatalf("failed to create cf1: %v", err)
	}
	err = reg.CreateCF("cf2")
	if err != nil {
		t.Fatalf("failed to create cf2: %v", err)
	}

	batch := NewCFWriteBatch()
	batch.AddPut("cf1", []byte("key1"), []byte("value1"))
	batch.AddPut("cf2", []byte("key2"), []byte("value2"))
	batch.AddDelete("cf1", []byte("key3"))

	if batch.Size() != 3 {
		t.Errorf("expected batch size 3, got %d", batch.Size())
	}

	// Применяем батч
	commitTS, err := ApplyCFBatch(reg, batch)
	if err != nil {
		t.Fatalf("failed to apply batch: %v", err)
	}
	if commitTS == 0 {
		t.Error("expected non-zero commit timestamp")
	}

	// Проверяем, что данные записались в правильные CF
	eng1, _ := reg.GetCF("cf1")
	val, err := eng1.GetWithTS([]byte("key1"), commitTS)
	if err != nil {
		t.Fatalf("failed to get key1 from cf1: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("expected value1, got %s", val)
	}

	eng2, _ := reg.GetCF("cf2")
	val, err = eng2.GetWithTS([]byte("key2"), commitTS)
	if err != nil {
		t.Fatalf("failed to get key2 from cf2: %v", err)
	}
	if string(val) != "value2" {
		t.Errorf("expected value2, got %s", val)
	}

	// Ключ key3 должен быть удалён (tombstone)
	val, err = eng1.GetWithTS([]byte("key3"), commitTS)
	if err != nil {
		t.Fatalf("failed to get key3: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil (tombstone), got %s", val)
	}
}

func TestCFWriteBatchEmpty(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	batch := NewCFWriteBatch()
	commitTS, err := ApplyCFBatch(reg, batch)
	if err != nil {
		t.Fatalf("ApplyCFBatch on empty batch should not error: %v", err)
	}
	if commitTS != 0 {
		t.Errorf("expected commitTS 0 for empty batch, got %d", commitTS)
	}
}

func TestCFWriteBatchClear(t *testing.T) {
	batch := NewCFWriteBatch()
	batch.AddPut("default", []byte("key"), []byte("value"))
	if batch.Size() != 1 {
		t.Fatalf("expected size 1")
	}
	batch.Clear()
	if batch.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", batch.Size())
	}
}

func TestCFWriteBatchNonExistentCF(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	batch := NewCFWriteBatch()
	batch.AddPut("nonexistent", []byte("key"), []byte("value"))
	_, err = ApplyCFBatch(reg, batch)
	if err == nil {
		t.Error("expected error when applying batch to non-existent CF")
	}
}