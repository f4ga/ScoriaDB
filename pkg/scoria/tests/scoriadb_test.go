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

package scoria_test

import (
	"scoriadb/pkg/scoria"
	"testing"
)

func TestScoriaDBCF(t *testing.T) {
	dir := t.TempDir()

	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to create ScoriaDB: %v", err)
	}
	defer db.Close()

	// Проверяем, что CF "default" существует
	val, err := db.Get([]byte("nonexistent"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for nonexistent key")
	}

	// Записываем в default
	err = db.Put([]byte("key1"), []byte("value1"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	val, err = db.Get([]byte("key1"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "value1" {
		t.Errorf("expected value1, got %s", val)
	}

	// Создаём новый CF
	err = db.CreateCF("mycf")
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	// Записываем в новый CF
	err = db.PutCF("mycf", []byte("key2"), []byte("value2"))
	if err != nil {
		t.Fatalf("PutCF failed: %v", err)
	}

	val, err = db.GetCF("mycf", []byte("key2"))
	if err != nil {
		t.Fatalf("GetCF failed: %v", err)
	}
	if string(val) != "value2" {
		t.Errorf("expected value2, got %s", val)
	}

	// Ключ из mycf не должен быть виден в default
	val, err = db.Get([]byte("key2"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil for key2 in default CF, got %s", val)
	}

	// Удаление из CF
	err = db.DeleteCF("mycf", []byte("key2"))
	if err != nil {
		t.Fatalf("DeleteCF failed: %v", err)
	}

	val, err = db.GetCF("mycf", []byte("key2"))
	if err != nil {
		t.Fatalf("GetCF after delete failed: %v", err)
	}
	if val != nil {
		t.Errorf("expected nil after delete, got %s", val)
	}

	// Список CF
	cfs := db.ListCFs()
	if len(cfs) != 2 {
		t.Errorf("expected 2 CFs, got %v", cfs)
	}
}

func TestScoriaDBDropCF(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.NewScoriaDB(dir)
	if err != nil {
		t.Fatalf("failed to create ScoriaDB: %v", err)
	}
	defer db.Close()

	err = db.CreateCF("todelete")
	if err != nil {
		t.Fatalf("CreateCF failed: %v", err)
	}

	// Записываем что-то
	err = db.PutCF("todelete", []byte("key"), []byte("value"))
	if err != nil {
		t.Fatalf("PutCF failed: %v", err)
	}

	// Удаляем CF
	err = db.DropCF("todelete")
	if err != nil {
		t.Fatalf("DropCF failed: %v", err)
	}

	// После удаления CF не должен существовать
	_, err = db.GetCF("todelete", []byte("key"))
	if err == nil {
		t.Error("expected error when accessing dropped CF")
	}
}

func TestScoriaDBEmbeddedCFDB(t *testing.T) {
	dir := t.TempDir()
	db, err := scoria.EmbeddedCFDB(dir)
	if err != nil {
		t.Fatalf("EmbeddedCFDB failed: %v", err)
	}
	defer db.Close()

	// Простая проверка работы
	err = db.Put([]byte("test"), []byte("data"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	val, err := db.Get([]byte("test"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "data" {
		t.Errorf("expected data, got %s", val)
	}
}
