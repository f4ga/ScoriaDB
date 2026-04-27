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
	"scoriadb/internal/mvcc"
	"testing"
)

func TestMemTablePutGet(t *testing.T) {
	mt := NewMemTable()

	key := []byte("test_key")
	value := []byte("test_value")
	ts := uint64(100)

	mvccKey := mvcc.NewMVCCKey(key, ts)
	mt.Put(mvccKey, value)

	got, found := mt.Get(mvccKey)
	if !found {
		t.Fatal("key not found")
	}
	if string(got) != string(value) {
		t.Errorf("expected %s, got %s", value, got)
	}
}

func TestMemTableMultipleVersions(t *testing.T) {
	mt := NewMemTable()

	key := []byte("key")
	v1 := []byte("value1")
	v2 := []byte("value2")
	v3 := []byte("value3")

	// Вставляем версии с разными timestamp
	mt.Put(mvcc.NewMVCCKey(key, 10), v1)
	mt.Put(mvcc.NewMVCCKey(key, 20), v2)
	mt.Put(mvcc.NewMVCCKey(key, 30), v3)

	// Запрос с snapshotTS = 25 должен вернуть v2 (последняя версия <= 25)
	k25 := mvcc.NewMVCCKey(key, 25)
	got, found := mt.Get(k25)
	if !found {
		t.Fatal("key not found for ts 25")
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}

	// Запрос с snapshotTS = 30 должен вернуть v3
	k30 := mvcc.NewMVCCKey(key, 30)
	got, found = mt.Get(k30)
	if !found {
		t.Fatal("key not found for ts 30")
	}
	if string(got) != string(v3) {
		t.Errorf("expected %s, got %s", v3, got)
	}

	// Запрос с snapshotTS = 5 должен не найти (версии только с 10)
	k5 := mvcc.NewMVCCKey(key, 5)
	_, found = mt.Get(k5)
	if found {
		t.Error("expected not found for ts 5")
	}
}

func TestMemTableIterator(t *testing.T) {
	mt := NewMemTable()

	// Вставляем несколько ключей
	mt.Put(mvcc.NewMVCCKey([]byte("a"), 10), []byte("val_a"))
	mt.Put(mvcc.NewMVCCKey([]byte("b"), 20), []byte("val_b"))
	mt.Put(mvcc.NewMVCCKey([]byte("c"), 30), []byte("val_c"))

	iter := mt.NewIterator()
	defer iter.Close()

	var keys []string
	for iter.Next() {
		keys = append(keys, string(iter.Key().Key))
	}

	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}
	// Порядок должен быть сортирован по ключу и timestamp
	expected := []string{"a", "b", "c"}
	for i, exp := range expected {
		if keys[i] != exp {
			t.Errorf("key %d: expected %s, got %s", i, exp, keys[i])
		}
	}
}

func TestMemTableSize(t *testing.T) {
	mt := NewMemTable()
	if mt.Size() != 0 {
		t.Errorf("initial size should be 0, got %d", mt.Size())
	}

	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 1), []byte("v1"))
	if mt.Size() != 1 {
		t.Errorf("size after one insert should be 1, got %d", mt.Size())
	}

	// Обновление того же ключа с тем же timestamp не должно увеличивать размер
	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 1), []byte("v2"))
	if mt.Size() != 1 {
		t.Errorf("size after update should still be 1, got %d", mt.Size())
	}

	// Новая версия того же ключа увеличивает размер? В нашей реализации - да, потому что это другая запись
	mt.Put(mvcc.NewMVCCKey([]byte("k1"), 2), []byte("v3"))
	if mt.Size() != 2 {
		t.Errorf("size after new version should be 2, got %d", mt.Size())
	}
}

// TestMVCCGetWithMultipleVersions проверяет корректность поиска видимой версии
// с учётом tombstone и различных snapshotTS.
func TestMVCCGetWithMultipleVersions(t *testing.T) {
	mt := NewMemTable()
	key := []byte("user1")

	// Вставляем три версии с разными commitTS
	v1 := []byte("value1")
	v2 := []byte("value2")
	v3 := []byte("value3")
	mt.Put(mvcc.NewMVCCKey(key, 10), v1)
	mt.Put(mvcc.NewMVCCKey(key, 20), v2)
	mt.Put(mvcc.NewMVCCKey(key, 30), v3)

	// snapshotTS=15 должен вернуть v1 (commitTS 10)
	got, found := mt.Get(mvcc.NewMVCCKey(key, 15))
	if !found {
		t.Fatal("key not found for snapshotTS=15")
	}
	if string(got) != string(v1) {
		t.Errorf("expected %s, got %s", v1, got)
	}

	// snapshotTS=25 должен вернуть v2 (commitTS 20)
	got, found = mt.Get(mvcc.NewMVCCKey(key, 25))
	if !found {
		t.Fatal("key not found for snapshotTS=25")
	}
	if string(got) != string(v2) {
		t.Errorf("expected %s, got %s", v2, got)
	}

	// snapshotTS=5 должен вернуть ErrKeyNotFound (все версии новее)
	_, found = mt.Get(mvcc.NewMVCCKey(key, 5))
	if found {
		t.Error("expected not found for snapshotTS=5")
	}

	// Проверка tombstone: удаляем ключ с commitTS=20 (используем DeleteWithTS)
	mt.DeleteWithTS(mvcc.NewMVCCKey(key, 20)) // tombstone
	// snapshotTS=25 должен вернуть ErrKeyNotFound (tombstone скрывает ключ)
	_, found = mt.Get(mvcc.NewMVCCKey(key, 25))
	if found {
		t.Error("expected not found for snapshotTS=25 after tombstone")
	}
	// snapshotTS=15 должен вернуть v1 (commitTS 10), потому что tombstone ещё не виден
	got, found = mt.Get(mvcc.NewMVCCKey(key, 15))
	if !found {
		t.Fatal("key not found for snapshotTS=15 after tombstone")
	}
	if string(got) != string(v1) {
		t.Errorf("expected %s, got %s", v1, got)
	}
}
