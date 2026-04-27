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