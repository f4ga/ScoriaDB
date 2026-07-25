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
	"fmt"
	"math"
	"sync/atomic"

	"github.com/f4ga/ScoriaDB/internal/cf"
	"github.com/f4ga/ScoriaDB/internal/engine"
	"github.com/f4ga/ScoriaDB/internal/txn"
)

// ============================================================
// Public Interfaces
// ============================================================

// DB is the base interface for operations without Column Families.
type DB interface {
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Close() error
}

// CFDB is the public interface with Column Families support.
type CFDB interface {
	Get(key []byte) ([]byte, error)
	Put(key, value []byte) error
	Delete(key []byte) error
	Scan(prefix []byte) Iterator

	GetCF(cf string, key []byte) ([]byte, error)
	PutCF(cf string, key, value []byte) error
	DeleteCF(cf string, key []byte) error
	ScanCF(cf string, prefix []byte) Iterator

	NewTransaction() Transaction
	NewBatch() Batch
	NewBatchForCF(cfName string) Batch

	CreateCF(name string) error
	DropCF(name string) error
	ListCFs() []string

	Close() error
}

// Iterator is the public iterator interface.
// Alias for engine.Iterator to avoid duplication.
type Iterator = engine.Iterator

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

// ============================================================
// Main ScoriaDB Type
// ============================================================

// ScoriaDB implements CFDB using the Column Family registry.
type ScoriaDB struct {
	registry *cf.Registry
}

// ============================================================
// Error Iterator
// ============================================================

// errorIterator returns an error on Err().
type errorIterator struct {
	err error
}

func (it *errorIterator) Next() bool      { return false }
func (it *errorIterator) Key() []byte     { return nil }
func (it *errorIterator) Value() []byte   { return nil }
func (it *errorIterator) Err() error      { return it.err }
func (it *errorIterator) Close() error    { return nil }   // ← ИСПРАВЛЕНО
func (it *errorIterator) IsDeleted() bool { return false } // ← НОВЫЙ МЕТОД

// ============================================================
// Scoria Merge Iterator — Wraps engine.Scan
// ============================================================

// scoriaMergeIter wraps engine's Scan iterator.
// All logic lives in engine package (DRY principle).
type scoriaMergeIter struct {
	inner engine.Iterator
	err   error
}

func (it *scoriaMergeIter) Next() bool {
	if it.err != nil {
		return false
	}
	if !it.inner.Next() {
		if err := it.inner.Err(); err != nil {
			it.err = err
		}
		return false
	}
	return true
}

func (it *scoriaMergeIter) Key() []byte     { return it.inner.Key() }
func (it *scoriaMergeIter) Value() []byte   { return it.inner.Value() }
func (it *scoriaMergeIter) Err() error      { return it.err }
func (it *scoriaMergeIter) Close() error    { return it.inner.Close() } // ← ИСПРАВЛЕНО
func (it *scoriaMergeIter) IsDeleted() bool { return false }            // ← НОВЫЙ МЕТОД

// newScoriaMergeIter delegates to engine.Scan.
// All logic is in engine package — no duplication.
func newScoriaMergeIter(eng *engine.LSMEngine, prefix []byte) *scoriaMergeIter {
	return &scoriaMergeIter{
		inner: eng.Scan(prefix),
	}
}

// ============================================================
// Error Transaction
// ============================================================

// errorTransaction always returns an error.
type errorTransaction struct {
	err error
}

func (tx *errorTransaction) Get(key []byte) ([]byte, error) { return nil, tx.err }
func (tx *errorTransaction) Put(key, value []byte) error    { return tx.err }
func (tx *errorTransaction) Delete(key []byte) error        { return tx.err }
func (tx *errorTransaction) Commit() error                  { return tx.err }
func (tx *errorTransaction) Rollback() error                { return nil }

// ============================================================
// Batch Implementation
// ============================================================

// scoriaBatch wraps txn.WriteBatch.
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

// ============================================================
// Constructors
// ============================================================

// NewScoriaDB creates a new ScoriaDB database.
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

// ============================================================
// Basic Operations (default CF)
// ============================================================

func (db *ScoriaDB) Get(key []byte) ([]byte, error) {
	return db.GetCF("default", key)
}

func (db *ScoriaDB) Put(key, value []byte) error {
	return db.PutCF("default", key, value)
}

func (db *ScoriaDB) Delete(key []byte) error {
	return db.DeleteCF("default", key)
}

func (db *ScoriaDB) Scan(prefix []byte) Iterator {
	return db.ScanCF("default", prefix)
}

// ============================================================
// Column Family Operations
// ============================================================

func (db *ScoriaDB) GetCF(cfName string, key []byte) ([]byte, error) {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return nil, err
	}

	_, ts, err := eng.GetLatestInfo(key)
	if err != nil {
		return nil, err
	}
	if ts == 0 {
		return nil, nil // key does not exist
	}

	val, err := eng.GetWithTS(key, math.MaxUint64)
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (db *ScoriaDB) PutCF(cfName string, key, value []byte) error {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return err
	}
	ts := atomic.AddUint64(&eng.LastTS, 1)
	return eng.PutWithTS(key, value, ts)
}

func (db *ScoriaDB) DeleteCF(cfName string, key []byte) error {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return err
	}
	ts := atomic.AddUint64(&eng.LastTS, 1)
	return eng.DeleteWithTS(key, ts)
}

func (db *ScoriaDB) ScanCF(cfName string, prefix []byte) Iterator {
	eng, err := db.registry.GetCF(cfName)
	if err != nil {
		return &errorIterator{err: fmt.Errorf("CF %q not found: %w", cfName, err)}
	}
	return newScoriaMergeIter(eng, prefix)
}

// ============================================================
// Column Family Administration
// ============================================================

func (db *ScoriaDB) CreateCF(name string) error {
	if name == "" {
		return fmt.Errorf("CF name cannot be empty")
	}
	return db.registry.CreateCF(name)
}

func (db *ScoriaDB) DropCF(name string) error {
	if name == "default" || name == "__auth__" {
		return fmt.Errorf("cannot drop system CF: %s", name)
	}
	return db.registry.DropCF(name)
}

func (db *ScoriaDB) ListCFs() []string {
	return db.registry.ListCFs()
}

// ============================================================
// Transactions and Batches
// ============================================================

func (db *ScoriaDB) NewTransaction() Transaction {
	eng, err := db.registry.GetCF("default")
	if err != nil {
		return &errorTransaction{err: err}
	}
	startTS := eng.NextTimestamp()
	return txn.Begin(eng, startTS)
}

func (db *ScoriaDB) NewBatch() Batch {
	return &scoriaBatch{
		db:     db,
		cfName: "default",
		inner:  txn.NewWriteBatch(),
	}
}

func (db *ScoriaDB) NewBatchForCF(cfName string) Batch {
	return &scoriaBatch{
		db:     db,
		cfName: cfName,
		inner:  txn.NewWriteBatch(),
	}
}

// ============================================================
// Close
// ============================================================

func (db *ScoriaDB) Close() error {
	return db.registry.Close()
}

// ============================================================
// Utilities
// ============================================================

func EmbeddedCFDB(dataDir string) (CFDB, error) {
	return NewScoriaDB(dataDir)
}

func Open(opts Options) (DB, error) {
	walOpts := engine.DefaultWALOptions()
	if opts.WALOptions != nil {
		walOpts = *opts.WALOptions
	}
	return NewScoriaDBWithOptions(opts.WorkDir, walOpts)
}
