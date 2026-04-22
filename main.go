package main

import (
	"fmt"
	"log"
	"tinytikv/memtable"
)

func main() {
	fmt.Println("=== TinyTiKV MemTable ===")
	fmt.Println("Простой запуск демонстрации работы MemTable с WAL")

	// Создаем MemTable с WAL файлом
	walPath := "wal.log"
	mt, err := memtable.NewMemTable(walPath)
	if err != nil {
		log.Fatalf("Ошибка создания MemTable: %v", err)
	}
	defer mt.Close()

	fmt.Printf("MemTable создана с WAL файлом: %s\n", walPath)
	fmt.Printf("Текущий размер: %d элементов\n", mt.Size())

	// Простая проверка работы
	fmt.Println("\n--- Базовая проверка ---")
	
	// Устанавливаем одно значение для демонстрации
	if err := mt.Set("demo_key", "demo_value"); err != nil {
		log.Printf("Ошибка при установке значения: %v", err)
	} else {
		fmt.Println("Установлено: demo_key = demo_value")
	}

	// Получаем значение
	if value, exists := mt.Get("demo_key"); exists {
		fmt.Printf("Получено: demo_key = %s\n", value)
	} else {
		fmt.Println("Ключ 'demo_key' не найден")
	}

	// Проверяем несуществующий ключ
	if _, exists := mt.Get("non_existent"); !exists {
		fmt.Println("Ключ 'non_existent' не найден (ожидаемо)")
	}

	fmt.Printf("\nФинальный размер: %d элементов\n", mt.Size())
	fmt.Println("\nДля полного тестирования запустите: go test ./memtable -v")
	fmt.Println("=== Завершено ===")
}