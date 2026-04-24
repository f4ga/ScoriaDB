package cf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRegistryCreateAndGet(t *testing.T) {
	dir := t.TempDir()

	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	// Проверяем, что CF "default" создан автоматически
	eng, err := reg.GetCF("default")
	if err != nil {
		t.Fatalf("expected default CF to exist: %v", err)
	}
	if eng == nil {
		t.Fatal("engine is nil")
	}

	// Создаём новый CF
	err = reg.CreateCF("testcf")
	if err != nil {
		t.Fatalf("failed to create testcf: %v", err)
	}

	// Получаем его
	eng2, err := reg.GetCF("testcf")
	if err != nil {
		t.Fatalf("failed to get testcf: %v", err)
	}
	if eng2 == nil {
		t.Fatal("engine is nil")
	}

	// Проверяем, что директория создана
	cfDir := filepath.Join(dir, "testcf")
	if _, err := os.Stat(cfDir); os.IsNotExist(err) {
		t.Errorf("CF directory not created: %s", cfDir)
	}

	// Список CF должен содержать оба
	cfs := reg.ListCFs()
	if len(cfs) != 2 {
		t.Errorf("expected 2 CFs, got %v", cfs)
	}
	// Порядок не гарантирован, проверяем наличие
	hasDefault := false
	hasTest := false
	for _, cf := range cfs {
		if cf == "default" {
			hasDefault = true
		}
		if cf == "testcf" {
			hasTest = true
		}
	}
	if !hasDefault || !hasTest {
		t.Errorf("missing expected CFs, got %v", cfs)
	}
}

func TestRegistryDuplicateCF(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	err = reg.CreateCF("mycf")
	if err != nil {
		t.Fatalf("failed to create mycf: %v", err)
	}

	// Попытка создать с тем же именем должна вернуть ошибку
	err = reg.CreateCF("mycf")
	if err == nil {
		t.Error("expected error when creating duplicate CF")
	}
}

func TestRegistryDropCF(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	err = reg.CreateCF("todelete")
	if err != nil {
		t.Fatalf("failed to create todelete: %v", err)
	}

	// Удаляем
	err = reg.DropCF("todelete")
	if err != nil {
		t.Fatalf("failed to drop CF: %v", err)
	}

	// После удаления CF не должен существовать
	_, err = reg.GetCF("todelete")
	if err == nil {
		t.Error("expected error when getting dropped CF")
	}
}

func TestRegistryDropSystemCF(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}
	defer reg.Close()

	// Попытка удалить системный CF должна вернуть ошибку
	err = reg.DropCF("__auth__")
	if err == nil {
		t.Error("expected error when dropping system CF")
	}
}

func TestRegistryClose(t *testing.T) {
	dir := t.TempDir()
	reg, err := NewRegistry(dir)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	err = reg.CreateCF("cf1")
	if err != nil {
		t.Fatalf("failed to create cf1: %v", err)
	}

	// Закрытие должно работать без ошибок
	err = reg.Close()
	if err != nil {
		t.Fatalf("close failed: %v", err)
	}

	// После закрытия операции должны возвращать ошибку (движки закрыты)
	// Но наш реестр не защищён от использования после закрытия, просто проверяем, что не паникует
}