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
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync/atomic"

	"scoriadb/internal/cf"
	"scoriadb/internal/engine"
	"scoriadb/internal/mvcc"
	"scoriadb/internal/txn"
)

// CFDB представляет публичный интерфейс базы данных ScoriaDB с поддержкой
// Column Families, транзакций, батчей и итераторов.
// Этот интерфейс соответствует требованиям Промпта 4 (Embedded Go API).
type CFDB interface {
	// Основные операции (по умолчанию в CF "default")
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Scan(prefix []byte) Iterator // итератор по ключам с префиксом

	// Операции с указанием Column Family
	GetCF(cf string, key []byte) ([]byte, error)
	PutCF(cf string, key, value []byte) error
	DeleteCF(cf string, key []byte) error
	ScanCF(cf string, prefix []byte) Iterator

	// Транзакции
	NewTransaction() Transaction
	NewBatch() Batch
	NewBatchForCF(cfName string) Batch

	// Администрирование Column Families
	CreateCF(name string) error
	DropCF(name string) error
	ListCFs() []string

	// Закрытие
	Close() error
}

// Iterator представляет итератор по ключам и значениям.
type Iterator interface {
	// Next перемещает итератор к следующей записи.
	// Возвращает false, если больше записей нет.
	Next() bool
	// Key возвращает ключ текущей записи.
	Key() []byte
	// Value возвращает значение текущей записи.
	Value() []byte
	// Err возвращает ошибку, возникшую при итерации.
	Err() error
	// Close освобождает ресурсы итератора.
	Close()
}

// Transaction представляет интерактивную транзакцию.
type Transaction interface {
	// Get возвращает значение ключа в контексте транзакции.
	Get(key []byte) ([]byte, error)
	// Put добавляет операцию записи в транзакцию.
	Put(key, value []byte) error
	// Delete добавляет операцию удаления в транзакцию.
	Delete(key []byte) error
	// Commit применяет все изменения атомарно.
	Commit() error
	// Rollback отменяет транзакцию.
	Rollback() error
}

// Batch представляет атомарный батч операций.
type Batch interface {
	// AddPut добавляет операцию записи в батч.
	AddPut(key, value []byte)
	// AddDelete добавляет операцию удаления в батч.
	AddDelete(key []byte)
	// Commit применяет все операции батча атомарно.
	Commit() error
	// Clear очищает батч.
	Clear()
	// Size возвращает количество операций в батче.
	Size() int
}

// ScoriaDB реализация CFDB с использованием реестра Column Families.
type ScoriaDB struct {
	registry *cf.Registry
}

// errorIterator — итератор, возвращающий ошибку при проверке через Err().
type errorIterator struct {
	err error
}

func (it *errorIterator) Next() bool    { return false }
func (it *errorIterator) Key() []byte   { return nil }
func (it *errorIterator) Value() []byte { return nil }
func (it *errorIterator) Err() error    { return it.err }
func (it *errorIterator) Close()        {}

// mergeIterator — итератор, объединяющий данные из активной MemTable, frozen MemTable и SSTable.
type mergeIterator struct {
	// собранные ключи (userKey -> latest MVCCKey)
	keys []mvcc.MVCCKey
	// сырые значения (inline или указатели на VLog)
	rawValues [][]byte
	// разрешённые значения (кеш)
	resolvedValues [][]byte
	// движок для чтения из VLog
	engine *engine.LSMEngine
	// текущий индекс
	index int
	// ошибка, возникшая при итерации
	err error
}

func (it *mergeIterator) Next() bool {
	it.index++
	return it.index < len(it.keys)
}

func (it *mergeIterator) Key() []byte {
	if it.index < 0 || it.index >= len(it.keys) {
		return nil
	}
	return it.keys[it.index].Key
}

func (it *mergeIterator) Value() []byte {
	if it.index < 0 || it.index >= len(it.rawValues) {
		return nil
	}

	// Если значение уже разрешено, возвращаем его
	if it.resolvedValues != nil && it.resolvedValues[it.index] != nil {
		return it.resolvedValues[it.index]
	}

	raw := it.rawValues[it.index]
	// Если длина 12 байт — это указатель на VLog
	if len(raw) == 12 {
		// Декодируем указатель: [fileID: 8 байт][offset: 4 байта]
		fileID := binary.BigEndian.Uint64(raw[0:8])
		offset := binary.BigEndian.Uint32(raw[8:12])

		// Читаем значение из VLog
		val, err := it.engine.ReadVLogValue(fileID, offset)
		if err != nil {
			it.err = err
			return nil
		}

		// Инициализируем кеш, если нужно
		if it.resolvedValues == nil {
			it.resolvedValues = make([][]byte, len(it.rawValues))
		}
		it.resolvedValues[it.index] = val
		return val
	}

	// Иначе это inline значение
	return raw
}

func (it *mergeIterator) Err() error {
	return it.err
}

func (it *mergeIterator) Close() {
	it.keys = nil
	it.rawValues = nil
	it.resolvedValues = nil
}

// newMergeIterator создаёт mergeIterator для указанного движка и префикса.
func newMergeIterator(eng *engine.LSMEngine, prefix []byte) *mergeIterator {
	// Собираем ключи из активной MemTable
	active := eng.ActiveMemTable()
	frozen := eng.FrozenMemTable()

	// map userKey -> latest MVCCKey (с максимальным timestamp)
	latestKeys := make(map[string]mvcc.MVCCKey)
	latestValues := make(map[string][]byte)

	// Функция для обработки итератора MemTable
	processMemTable := func(mt *engine.MemTable) {
		if mt == nil {
			return
		}
		iter := mt.NewIterator()
		defer iter.Close()
		for iter.Next() {
			key := iter.Key()
			userKey := key.Key
			if !bytes.HasPrefix(userKey, prefix) {
				continue
			}
			// Пропускаем tombstone (значение nil)
			value := iter.Value()
			if value == nil {
				continue
			}
			// Проверяем, есть ли уже более новая версия
			existing, ok := latestKeys[string(userKey)]
			if !ok || key.Timestamp > existing.Timestamp {
				latestKeys[string(userKey)] = key
				latestValues[string(userKey)] = value
			}
		}
	}

	processMemTable(active)
	processMemTable(frozen)

	// TODO: добавить SSTable

	// Сортируем ключи по userKey
	userKeys := make([]string, 0, len(latestKeys))
	for uk := range latestKeys {
		userKeys = append(userKeys, uk)
	}
	sort.Strings(userKeys)

	// Строим slices для итератора
	keys := make([]mvcc.MVCCKey, len(userKeys))
	rawValues := make([][]byte, len(userKeys))
	for i, uk := range userKeys {
		keys[i] = latestKeys[uk]
		rawValues[i] = latestValues[uk]
	}

	return &mergeIterator{
		keys:      keys,
		rawValues: rawValues,
		engine:    eng,
		index:     -1,
	}
}

// errorTransaction — транзакция, которая всегда возвращает ошибку.
type errorTransaction struct {
	err error
}

func (tx *errorTransaction) Get(key []byte) ([]byte, error) { return nil, tx.err }
func (tx *errorTransaction) Put(key, value []byte) error    { return tx.err }
func (tx *errorTransaction) Delete(key []byte) error        { return tx.err }
func (tx *errorTransaction) Commit() error                  { return tx.err }
func (tx *errorTransaction) Rollback() error                { return nil }

// scoriaBatch — обёртка над txn.WriteBatch, привязанная к конкретному ScoriaDB и CF.
type scoriaBatch struct {
	db     *ScoriaDB
	cfName string
	inner  *txn.WriteBatch
}

func (b *scoriaBatch) AddPut(key, value []byte) {
	b.inner.AddPut(key, value)
}

func (b *scoriaBatch) AddDelete(key []byte) {
	b.inner.AddDelete(key)
}

func (b *scoriaBatch) Commit() error {
	// Получаем движок для указанного CF
	eng, err := b.db.registry.GetCF(b.cfName)
	if err != nil {
		return err
	}
	// Применяем батч
	_, err = txn.ApplyBatch(eng, b.inner)
	return err
}

func (b *scoriaBatch) Clear() {
	b.inner.Clear()
}

func (b *scoriaBatch) Size() int {
	return b.inner.Size()
}

// NewScoriaDB создаёт новую базу данных ScoriaDB с поддержкой Column Families.
// dataDir — корневая директория, где будут храниться данные всех CF.
func NewScoriaDB(dataDir string) (*ScoriaDB, error) {
	reg, err := cf.NewRegistry(dataDir)
	if err != nil {
		return nil, err
	}
	return &ScoriaDB{registry: reg}, nil
}

// Get возвращает значение по ключу из CF "default".
func (db *ScoriaDB) Get(key []byte) ([]byte, error) {
	return db.GetCF("default", key)
}

// Put записывает значение по ключу в CF "default".
func (db *ScoriaDB) Put(key, value []byte) error {
	return db.PutCF("default", key, value)
}

// Delete удаляет ключ из CF "default".
func (db *ScoriaDB) Delete(key []byte) error {
	return db.DeleteCF("default", key)
}

// GetCF возвращает значение по ключу из указанного Column Family.
func (db *ScoriaDB) GetCF(cfName string, key []byte) ([]byte, error) {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return nil, err
	}
	// Используем максимальный timestamp для получения последней версии
	return eng.GetWithTS(key, math.MaxUint64)
}

// PutCF записывает значение по ключу в указанное Column Family.
func (db *ScoriaDB) PutCF(cfName string, key, value []byte) error {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return err
	}
	ts := atomic.AddUint64(&eng.LastTS, 1)
	return eng.PutWithTS(key, value, ts)
}

// DeleteCF удаляет ключ из указанного Column Family.
func (db *ScoriaDB) DeleteCF(cfName string, key []byte) error {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return err
	}
	ts := atomic.AddUint64(&eng.LastTS, 1)
	return eng.DeleteWithTS(key, ts)
}

// CreateCF создаёт новое Column Family.
func (db *ScoriaDB) CreateCF(name string) error {
	return db.registry.CreateCF(name)
}

// DropCF удаляет Column Family.
func (db *ScoriaDB) DropCF(name string) error {
	return db.registry.DropCF(name)
}

// ListCFs возвращает список имён всех Column Families.
func (db *ScoriaDB) ListCFs() []string {
	return db.registry.ListCFs()
}

// Scan возвращает итератор по ключам с префиксом в CF "default".
func (db *ScoriaDB) Scan(prefix []byte) Iterator {
	return db.ScanCF("default", prefix)
}

// ScanCF возвращает итератор по ключам с префиксом в указанном Column Family.
func (db *ScoriaDB) ScanCF(cfName string, prefix []byte) Iterator {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		// Возвращаем итератор с ошибкой, чтобы вызывающий код мог понять причину.
		return &errorIterator{err: fmt.Errorf("CF %q not found: %w", cfName, err)}
	}
	return newMergeIterator(eng, prefix)
}

// NewTransaction создаёт новую транзакцию.
func (db *ScoriaDB) NewTransaction() Transaction {
	// Пока создаём транзакцию на движке CF "default".
	eng, err := db.registry.GetCF("default")
	if err != nil {
		// Если default CF не существует (маловероятно), возвращаем транзакцию-заглушку
		return &errorTransaction{err: err}
	}
	// Используем внутренний пакет txn для создания транзакции
	startTS := uint64(0) // временно
	return txn.Begin(eng, startTS)
}

// NewBatch creates a new batch of operations bound to CF "default".
func (db *ScoriaDB) NewBatch() Batch {
	return &scoriaBatch{
		db:     db,
		cfName: "default",
		inner:  txn.NewWriteBatch(),
	}
}

// NewBatchForCF creates a new batch of operations bound to the specified CF.
func (db *ScoriaDB) NewBatchForCF(cfName string) Batch {
	return &scoriaBatch{
		db:     db,
		cfName: cfName,
		inner:  txn.NewWriteBatch(),
	}
}

// Close закрывает все Column Families и освобождает ресурсы.
func (db *ScoriaDB) Close() error {
	return db.registry.Close()
}

// EmbeddedCFDB возвращает интерфейс CFDB для встраивания.
func EmbeddedCFDB(dataDir string) (CFDB, error) {
	return NewScoriaDB(dataDir)
}

// Open открывает (или создаёт) базу данных ScoriaDB с указанными настройками.
// Возвращает интерфейс DB, соответствующий требованиям Промпта 4.
func Open(opts Options) (DB, error) {
	// Пока игнорируем все настройки кроме WorkDir
	return NewScoriaDB(opts.WorkDir)
}
