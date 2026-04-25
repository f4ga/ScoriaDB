package memtable

import (
	"os"
	"testing"
)

func TestMemTable_SetGet(t *testing.T) {
	// Создаем временный файл WAL
	walPath := "test_wal.log"
	defer os.Remove(walPath)

	mt, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create MemTable: %v", err)
	}
	defer mt.Close()

	// Тест Set и Get
	key := "test_key"
	value := "test_value"

	if err := mt.Set(key, value); err != nil {
		t.Errorf("Set failed: %v", err)
	}

	retrieved, exists := mt.Get(key)
	if !exists {
		t.Error("Get returned false for existing key")
	}
	if retrieved != value {
		t.Errorf("Get returned wrong value: got %s, want %s", retrieved, value)
	}

	// Тест Get для несуществующего ключа
	_, exists = mt.Get("non_existent")
	if exists {
		t.Error("Get returned true for non-existent key")
	}
}

func TestMemTable_Delete(t *testing.T) {
	walPath := "test_wal_delete.log"
	defer os.Remove(walPath)

	mt, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create MemTable: %v", err)
	}
	defer mt.Close()

	key := "to_delete"
	value := "value"

	if err := mt.Set(key, value); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Проверяем, что ключ существует
	if _, exists := mt.Get(key); !exists {
		t.Fatal("Key should exist before deletion")
	}

	// Удаляем ключ
	if err := mt.Delete(key); err != nil {
		t.Errorf("Delete failed: %v", err)
	}

	// Проверяем, что ключ удален
	if _, exists := mt.Get(key); exists {
		t.Error("Key should not exist after deletion")
	}
}

func TestMemTable_WALRecovery(t *testing.T) {
	walPath := "test_wal_recovery.log"
	defer os.Remove(walPath)

	// Создаем первую MemTable и записываем данные
	mt1, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create first MemTable: %v", err)
	}

	// Записываем несколько ключей
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range testData {
		if err := mt1.Set(k, v); err != nil {
			t.Errorf("Set failed for %s: %v", k, err)
		}
	}

	// Удаляем один ключ
	if err := mt1.Delete("key2"); err != nil {
		t.Errorf("Delete failed: %v", err)
	}

	mt1.Close()

	// Создаем вторую MemTable с тем же WAL файлом (восстановление)
	mt2, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create second MemTable: %v", err)
	}
	defer mt2.Close()

	// Проверяем восстановленные данные
	for k, expected := range testData {
		actual, exists := mt2.Get(k)
		if k == "key2" {
			// key2 должен быть удален
			if exists {
				t.Errorf("Key %s should not exist after deletion", k)
			}
		} else {
			if !exists {
				t.Errorf("Key %s should exist after recovery", k)
			}
			if actual != expected {
				t.Errorf("Wrong value for key %s: got %s, want %s", k, actual, expected)
			}
		}
	}

	// Проверяем размер
	if size := mt2.Size(); size != 2 {
		t.Errorf("Wrong size after recovery: got %d, want 2", size)
	}
}

func TestMemTable_ConcurrentAccess(t *testing.T) {
	walPath := "test_wal_concurrent.log"
	defer os.Remove(walPath)

	mt, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create MemTable: %v", err)
	}
	defer mt.Close()

	// Запускаем несколько горутин для конкурентного доступа
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			key := string(rune('a' + idx))
			value := string(rune('A' + idx))

			if err := mt.Set(key, value); err != nil {
				t.Errorf("Concurrent Set failed: %v", err)
			}

			retrieved, exists := mt.Get(key)
			if !exists || retrieved != value {
				t.Errorf("Concurrent Get failed for key %s", key)
			}
			done <- true
		}(i)
	}

	// Ожидаем завершения всех горутин
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestMemTable_Size(t *testing.T) {
	walPath := "test_wal_size.log"
	defer os.Remove(walPath)

	mt, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create MemTable: %v", err)
	}
	defer mt.Close()

	// Начальный размер должен быть 0
	if size := mt.Size(); size != 0 {
		t.Errorf("Initial size should be 0, got %d", size)
	}

	// Добавляем элементы
	if err := mt.Set("a", "1"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if size := mt.Size(); size != 1 {
		t.Errorf("Size after one insert should be 1, got %d", size)
	}

	if err := mt.Set("b", "2"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if err := mt.Set("c", "3"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	if size := mt.Size(); size != 3 {
		t.Errorf("Size after three inserts should be 3, got %d", size)
	}

	// Удаляем элемент
	if err := mt.Delete("b"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if size := mt.Size(); size != 2 {
		t.Errorf("Size after deletion should be 2, got %d", size)
	}
}

func TestMemTable_AliceData(t *testing.T) {
	walPath := "test_wal_alice.log"
	defer os.Remove(walPath)

	mt, err := NewMemTable(walPath)
	if err != nil {
		t.Fatalf("Failed to create MemTable: %v", err)
	}
	defer mt.Close()

	// Добавляем данные об Алисе
	aliceData := map[string]string{
		"name":       "Alice",
		"age":        "30",
		"profession": "Software Engineer",
		"city":       "Moscow",
		"hobby":      "Golang programming",
	}

	// Записываем все данные
	for key, value := range aliceData {
		if err := mt.Set(key, value); err != nil {
			t.Errorf("Failed to set %s: %v", key, err)
		}
	}

	// Проверяем, что все данные сохранились
	for key, expectedValue := range aliceData {
		actualValue, exists := mt.Get(key)
		if !exists {
			t.Errorf("Key %s should exist", key)
			continue
		}
		if actualValue != expectedValue {
			t.Errorf("Wrong value for %s: got %s, want %s", key, actualValue, expectedValue)
		}
	}

	// Проверяем размер
	if size := mt.Size(); size != len(aliceData) {
		t.Errorf("Wrong size: got %d, want %d", size, len(aliceData))
	}

	// Обновляем одно значение
	if err := mt.Set("age", "31"); err != nil {
		t.Errorf("Failed to update age: %v", err)
	}

	// Проверяем обновление
	if age, exists := mt.Get("age"); !exists || age != "31" {
		t.Errorf("Age not updated correctly: got %s, want 31", age)
	}

	// Удаляем один ключ
	if err := mt.Delete("hobby"); err != nil {
		t.Errorf("Failed to delete hobby: %v", err)
	}

	// Проверяем удаление
	if _, exists := mt.Get("hobby"); exists {
		t.Error("Hobby should be deleted")
	}

	// Проверяем финальный размер
	if size := mt.Size(); size != len(aliceData)-1 {
		t.Errorf("Wrong final size: got %d, want %d", size, len(aliceData)-1)
	}
}