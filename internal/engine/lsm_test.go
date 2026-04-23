package engine

import (
	"testing"
)

func TestLSMEnginePutGet(t *testing.T) {
	dir := t.TempDir()

	engine, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	// Простая запись и чтение
	key := []byte("test_key")
	value := []byte("test_value")
	if err := engine.PutWithTS(key, value, 1); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	got, err := engine.GetWithTS(key, 1)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}

	// Чтение с более новым snapshot должно тоже найти (последняя версия)
	got, err = engine.GetWithTS(key, 100)
	if err != nil {
		t.Fatalf("failed to get with newer ts: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}
}

func TestLSMEngineMultipleVersions(t *testing.T) {
	dir := t.TempDir()

	engine, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	key := []byte("key")
	v1 := []byte("value1")
	v2 := []byte("value2")

	if err := engine.PutWithTS(key, v1, 10); err != nil {
		t.Fatalf("failed to put v1: %v", err)
	}
	if err := engine.PutWithTS(key, v2, 20); err != nil {
		t.Fatalf("failed to put v2: %v", err)
	}

	// snapshotTS = 15 должен вернуть v1
	got, err := engine.GetWithTS(key, 15)
	if err != nil {
		t.Fatalf("failed to get at ts 15: %v", err)
	}
	if string(got) != string(v1) {
		t.Errorf("expected %s, got %s", v1, got)
	}

	// snapshotTS = 20 должен вернуть v2
	got, err = engine.GetWithTS(key, 20)
	if err != nil {
		t.Fatalf("failed to get at ts 20: %v", err)
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}
}

func TestLSMEngineDelete(t *testing.T) {
	dir := t.TempDir()

	engine, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	key := []byte("key")
	value := []byte("value")

	if err := engine.PutWithTS(key, value, 10); err != nil {
		t.Fatalf("failed to put: %v", err)
	}
	if err := engine.DeleteWithTS(key, 20); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// До удаления ключ существует
	got, err := engine.GetWithTS(key, 15)
	if err != nil {
		t.Fatalf("failed to get before delete: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}

	// После удаления ключ не должен находиться
	got, err = engine.GetWithTS(key, 25)
	if err != nil {
		t.Fatalf("failed to get after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %s", got)
	}
}

func TestLSMEngineRecovery(t *testing.T) {
	dir := t.TempDir()

	// Создаем движок, пишем данные, закрываем
	engine1, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine1: %v", err)
	}
	key := []byte("persistent_key")
	value := []byte("persistent_value")
	if err := engine1.PutWithTS(key, value, 42); err != nil {
		t.Fatalf("failed to put: %v", err)
	}
	engine1.Close()

	// Создаем новый движок в той же директории (должен восстановить данные)
	engine2, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine2: %v", err)
	}
	defer engine2.Close()

	got, err := engine2.GetWithTS(key, 42)
	if err != nil {
		t.Fatalf("failed to get after recovery: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}
}

func TestLSMEngineVLogLargeValue(t *testing.T) {
	dir := t.TempDir()

	engine, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	defer engine.Close()

	// Значение больше MaxInlineSize (64 байта)
	largeValue := make([]byte, 100)
	for i := range largeValue {
		largeValue[i] = byte(i)
	}
	key := []byte("large_key")

	if err := engine.PutWithTS(key, largeValue, 1); err != nil {
		t.Fatalf("failed to put large value: %v", err)
	}

	got, err := engine.GetWithTS(key, 1)
	if err != nil {
		t.Fatalf("failed to get large value: %v", err)
	}
	if len(got) != len(largeValue) {
		t.Fatalf("length mismatch: expected %d, got %d", len(largeValue), len(got))
	}
	for i := range largeValue {
		if got[i] != largeValue[i] {
			t.Errorf("byte mismatch at index %d: expected %d, got %d", i, largeValue[i], got[i])
		}
	}
}