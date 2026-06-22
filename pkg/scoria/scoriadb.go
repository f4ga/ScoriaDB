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

	"github.com/f4ga/ScoriaDB/internal/cf"
	"github.com/f4ga/ScoriaDB/internal/engine"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
	"github.com/f4ga/ScoriaDB/internal/txn"
)

// DB is the base interface for operations without Column Families (default CF only).
type DB interface {
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Close() error
}

// CFDB represents the public interface of ScoriaDB with support for
// Column Families, transactions, batches, and iterators.
type CFDB interface {
	// Basic operations (default CF)
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Scan(prefix []byte) Iterator

	// Column Family operations
	GetCF(cf string, key []byte) ([]byte, error)
	PutCF(cf string, key, value []byte) error
	DeleteCF(cf string, key []byte) error
	ScanCF(cf string, prefix []byte) Iterator

	// Transactions and batches
	NewTransaction() Transaction
	NewBatch() Batch
	NewBatchForCF(cfName string) Batch

	// Column Family administration
	CreateCF(name string) error
	DropCF(name string) error
	ListCFs() []string

	// Close
	Close() error
}

// Iterator iterates over keys and values.
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Err() error
	Close()
}

// Transaction represents an interactive transaction.
type Transaction interface {
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Commit() error
	Rollback() error
}

// Batch represents an atomic batch of operations.
type Batch interface {
	AddPut(key, value []byte)
	AddDelete(key []byte)
	Commit() error
	Clear()
	Size() int
}

// ScoriaDB implements CFDB using the Column Family registry.
type ScoriaDB struct {
	registry *cf.Registry
}

// errorIterator returns an error on Err().
type errorIterator struct {
	err error
}

func (it *errorIterator) Next() bool    { return false }
func (it *errorIterator) Key() []byte   { return nil }
func (it *errorIterator) Value() []byte { return nil }
func (it *errorIterator) Err() error    { return it.err }
func (it *errorIterator) Close()        {}

// mergeIterator combines data from active and frozen MemTables and SSTables.
type mergeIterator struct {
	keys           []mvcc.MVCCKey
	rawValues      [][]byte
	resolvedValues [][]byte
	engine         *engine.LSMEngine
	index          int
	err            error
	views          []*engine.VLogView // zero-copy views for VLog values
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

	if it.resolvedValues != nil && it.resolvedValues[it.index] != nil {
		return it.resolvedValues[it.index]
	}

	raw := it.rawValues[it.index]
	if len(raw) == 12 {
		fileID := binary.BigEndian.Uint64(raw[0:8])
		offset := binary.BigEndian.Uint32(raw[8:12])

		// zero-copy read using VLogView
		view, err := it.engine.ReadVLogView(fileID, offset)
		if err != nil {
			it.err = err
			return nil
		}

		if it.views == nil {
			it.views = make([]*engine.VLogView, 0)
		}
		it.views = append(it.views, view)

		if it.resolvedValues == nil {
			it.resolvedValues = make([][]byte, len(it.rawValues))
		}
		it.resolvedValues[it.index] = view.Data()
		return view.Data()
	}

	return raw
}

func (it *mergeIterator) Err() error {
	return it.err
}

func (it *mergeIterator) Close() {
	// Release all VLog views
	if it.views != nil {
		for _, view := range it.views {
			if view != nil {
				view.Release()
			}
		}
		it.views = nil
	}
	it.keys = nil
	it.rawValues = nil
	it.resolvedValues = nil
}

// newMergeIterator creates a mergeIterator for the given engine and prefix.
func newMergeIterator(eng *engine.LSMEngine, prefix []byte) *mergeIterator {
	active := eng.ActiveMemTable()
	frozen := eng.FrozenMemTable()

	latestKeys := make(map[string]mvcc.MVCCKey)
	latestValues := make(map[string][]byte)

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
			value := iter.Value()
			if value == nil {
				continue
			}
			existing, ok := latestKeys[string(userKey)]
			if !ok || key.Timestamp > existing.Timestamp {
				latestKeys[string(userKey)] = key
				latestValues[string(userKey)] = value
			}
		}
	}

	processMemTable(active)
	processMemTable(frozen)

	// TODO: add SSTable

	userKeys := make([]string, 0, len(latestKeys))
	for uk := range latestKeys {
		userKeys = append(userKeys, uk)
	}
	sort.Strings(userKeys)

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

// errorTransaction always returns an error.
type errorTransaction struct {
	err error
}

func (tx *errorTransaction) Get(key []byte) ([]byte, error) { return nil, tx.err }
func (tx *errorTransaction) Put(key, value []byte) error    { return tx.err }
func (tx *errorTransaction) Delete(key []byte) error        { return tx.err }
func (tx *errorTransaction) Commit() error                  { return tx.err }
func (tx *errorTransaction) Rollback() error                { return nil }

// scoriaBatch wraps txn.WriteBatch bound to a specific ScoriaDB and CF.
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
	eng, err := b.db.registry.GetCF(b.cfName)
	if err != nil {
		return err
	}
	_, err = txn.ApplyBatch(eng, b.inner)
	return err
}

func (b *scoriaBatch) Clear() {
	b.inner.Clear()
}

func (b *scoriaBatch) Size() int {
	return b.inner.Size()
}

// NewScoriaDB creates a new ScoriaDB database with Column Family support.
func NewScoriaDB(dataDir string) (*ScoriaDB, error) {
	return NewScoriaDBWithOptions(dataDir, engine.DefaultWALOptions())
}

// NewScoriaDBWithOptions creates a new ScoriaDB with the specified WAL options.
func NewScoriaDBWithOptions(dataDir string, walOpts engine.WALOptions) (*ScoriaDB, error) {
	reg, err := cf.NewRegistryWithOptions(dataDir, walOpts)
	if err != nil {
		return nil, err
	}
	return &ScoriaDB{registry: reg}, nil
}

// Get returns the value for a key from the default CF.
func (db *ScoriaDB) Get(key []byte) ([]byte, error) {
	return db.GetCF("default", key)
}

// Put writes a key-value pair to the default CF.
func (db *ScoriaDB) Put(key, value []byte) error {
	return db.PutCF("default", key, value)
}

// Delete removes a key from the default CF.
func (db *ScoriaDB) Delete(key []byte) error {
	return db.DeleteCF("default", key)
}

func (db *ScoriaDB) GetCF(cfName string, key []byte) ([]byte, error) {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return nil, err
	}

	// Проверяем существование ключа через GetLatestInfo
	_, ts, err := eng.GetLatestInfo(key)
	if err != nil {
		return nil, err
	}
	if ts == 0 {
		// Ключ не существует
		return nil, nil
	}

	// Ключ существует, получаем значение
	val, err := eng.GetWithTS(key, math.MaxUint64)
	if err != nil {
		return nil, err
	}

	// Если val == nil — значение пустое, возвращаем []byte{}
	if val == nil {
		return []byte{}, nil
	}
	return val, nil
}

// PutCF writes a key-value pair to the specified Column Family.
func (db *ScoriaDB) PutCF(cfName string, key, value []byte) error {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return err
	}
	ts := atomic.AddUint64(&eng.LastTS, 1)
	return eng.PutWithTS(key, value, ts)
}

// DeleteCF removes a key from the specified Column Family.
func (db *ScoriaDB) DeleteCF(cfName string, key []byte) error {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return err
	}
	ts := atomic.AddUint64(&eng.LastTS, 1)
	return eng.DeleteWithTS(key, ts)
}

// CreateCF creates a new Column Family.
func (db *ScoriaDB) CreateCF(name string) error {
	if name == "" {
		return fmt.Errorf("CF name cannot be empty")
	}
	return db.registry.CreateCF(name)
}

// DropCF drops a Column Family. System CFs cannot be dropped.
func (db *ScoriaDB) DropCF(name string) error {
	if name == "default" || name == "__auth__" {
		return fmt.Errorf("cannot drop system CF: %s", name)
	}
	return db.registry.DropCF(name)
}

// ListCFs returns a list of all Column Family names.
func (db *ScoriaDB) ListCFs() []string {
	return db.registry.ListCFs()
}

// Scan returns an iterator over keys with the given prefix in the default CF.
func (db *ScoriaDB) Scan(prefix []byte) Iterator {
	return db.ScanCF("default", prefix)
}

// ScanCF returns an iterator over keys with the given prefix in the specified CF.
func (db *ScoriaDB) ScanCF(cfName string, prefix []byte) Iterator {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return &errorIterator{err: fmt.Errorf("CF %q not found: %w", cfName, err)}
	}
	return newMergeIterator(eng, prefix)
}

// NewTransaction creates a new transaction on the default CF.
func (db *ScoriaDB) NewTransaction() Transaction {
	eng, err := db.registry.GetCF("default")
	if err != nil {
		return &errorTransaction{err: err}
	}
	startTS := eng.NextTimestamp()
	return txn.Begin(eng, startTS)
}

// NewBatch creates a new batch of operations bound to the default CF.
func (db *ScoriaDB) NewBatch() Batch {
	return &scoriaBatch{
		db:     db,
		cfName: "default",
		inner:  txn.NewWriteBatch(),
	}
}

// NewBatchForCF creates a new batch bound to the specified CF.
func (db *ScoriaDB) NewBatchForCF(cfName string) Batch {
	return &scoriaBatch{
		db:     db,
		cfName: cfName,
		inner:  txn.NewWriteBatch(),
	}
}

// Close closes all Column Families and releases resources.
func (db *ScoriaDB) Close() error {
	return db.registry.Close()
}

// EmbeddedCFDB returns a CFDB interface for embedding.
func EmbeddedCFDB(dataDir string) (CFDB, error) {
	return NewScoriaDB(dataDir)
}

// Open opens (or creates) a ScoriaDB database with the given options.
func Open(opts Options) (DB, error) {
	walOpts := engine.DefaultWALOptions()
	if opts.WALOptions != nil {
		walOpts = *opts.WALOptions
	}
	return NewScoriaDBWithOptions(opts.WorkDir, walOpts)
}
