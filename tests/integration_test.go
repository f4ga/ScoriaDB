package tests

import (
	"testing"
	"scoriadb/pkg/scoria"
)

func TestIntegration(t *testing.T) {
	dir := t.TempDir()

	// Открываем БД
	db, err := scoria.Open(scoria.DefaultOptions(dir))
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	// Записываем ключ
	key := []byte("testkey")
	value := []byte("testvalue")
	if err := db.Put(key, value); err != nil {
		t.Fatalf("failed to put: %v", err)
	}

	// Читаем ключ
	got, err := db.Get(key)
	if err != nil {
		t.Fatalf("failed to get: %v", err)
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}

	// Удаляем ключ
	if err := db.Delete(key); err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Проверяем, что ключ удалён
	got, err = db.Get(key)
	if err != nil {
		t.Fatalf("failed to get after delete: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil after delete, got %s", got)
	}
}