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

package scoria

import (
	"math"
	"scoriadb/internal/engine"
	"sync/atomic"
)

// DB представляет публичный интерфейс базы данных ScoriaDB.
type DB interface {
	// Get возвращает значение по ключу (последнюю версию).
	Get(key []byte) ([]byte, error)
	// Put записывает значение по ключу с автоматической генерацией timestamp.
	Put(key, value []byte) error
	// Delete удаляет ключ (помечает tombstone).
	Delete(key []byte) error
	// Close освобождает ресурсы базы данных.
	Close() error
}

// LSMDB реализация DB на основе LSM-движка.
type LSMDB struct {
	engine *engine.LSMEngine
}

// NewLSMDB создает новую базу данных с указанным путем к данным.
func NewLSMDB(dataDir string) (*LSMDB, error) {
	eng, err := engine.NewLSMEngine(dataDir)
	if err != nil {
		return nil, err
	}
	return &LSMDB{engine: eng}, nil
}

// Get возвращает значение по ключу (последнюю версицию).
func (db *LSMDB) Get(key []byte) ([]byte, error) {
	// Используем максимальный timestamp для получения последней версии
	return db.engine.GetWithTS(key, math.MaxUint64)
}

// Put записывает значение по ключу.
func (db *LSMDB) Put(key, value []byte) error {
	// Генерируем новый timestamp
	ts := atomic.AddUint64(&db.engine.LastTS, 1)
	return db.engine.PutWithTS(key, value, ts)
}

// Delete удаляет ключ (помечает tombstone).
func (db *LSMDB) Delete(key []byte) error {
	ts := atomic.AddUint64(&db.engine.LastTS, 1)
	return db.engine.DeleteWithTS(key, ts)
}

// Close освобождает ресурсы.
func (db *LSMDB) Close() error {
	return db.engine.Close()
}

// EmbeddedDB возвращает интерфейс DB для встраивания.
func EmbeddedDB(dataDir string) (DB, error) {
	return NewLSMDB(dataDir)
}
