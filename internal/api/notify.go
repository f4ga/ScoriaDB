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

package api

import (
	"scoriadb/pkg/scoria"
	"scoriadb/internal/api/ws"
)

// NotifyingDB оборачивает CFDB и отправляет уведомления в WebSocket‑хаб при записи/удалении.
type NotifyingDB struct {
	inner scoria.CFDB
	hub   *ws.Hub
}

// NewNotifyingDB создаёт обёртку над CFDB с поддержкой уведомлений.
func NewNotifyingDB(db scoria.CFDB, hub *ws.Hub) *NotifyingDB {
	return &NotifyingDB{inner: db, hub: hub}
}

// Get делегирует вызов внутренней БД.
func (ndb *NotifyingDB) Get(key []byte) ([]byte, error) {
	return ndb.inner.Get(key)
}

// Put записывает ключ и отправляет уведомление.
func (ndb *NotifyingDB) Put(key, value []byte) error {
	err := ndb.inner.Put(key, value)
	if err == nil {
		ndb.hub.NotifyKeyUpdated("default", string(key), value)
	}
	return err
}

// Delete удаляет ключ и отправляет уведомление.
func (ndb *NotifyingDB) Delete(key []byte) error {
	err := ndb.inner.Delete(key)
	if err == nil {
		ndb.hub.NotifyKeyDeleted("default", string(key))
	}
	return err
}

// Scan делегирует вызов внутренней БД.
func (ndb *NotifyingDB) Scan(prefix []byte) scoria.Iterator {
	return ndb.inner.Scan(prefix)
}

// GetCF делегирует вызов внутренней БД.
func (ndb *NotifyingDB) GetCF(cf string, key []byte) ([]byte, error) {
	return ndb.inner.GetCF(cf, key)
}

// PutCF записывает ключ в указанное CF и отправляет уведомление.
func (ndb *NotifyingDB) PutCF(cf string, key, value []byte) error {
	err := ndb.inner.PutCF(cf, key, value)
	if err == nil {
		ndb.hub.NotifyKeyUpdated(cf, string(key), value)
	}
	return err
}

// DeleteCF удаляет ключ из указанного CF и отправляет уведомление.
func (ndb *NotifyingDB) DeleteCF(cf string, key []byte) error {
	err := ndb.inner.DeleteCF(cf, key)
	if err == nil {
		ndb.hub.NotifyKeyDeleted(cf, string(key))
	}
	return err
}

// ScanCF делегирует вызов внутренней БД.
func (ndb *NotifyingDB) ScanCF(cf string, prefix []byte) scoria.Iterator {
	return ndb.inner.ScanCF(cf, prefix)
}

// NewTransaction делегирует вызов внутренней БД.
func (ndb *NotifyingDB) NewTransaction() scoria.Transaction {
	return ndb.inner.NewTransaction()
}

// NewBatch делегирует вызов внутренней БД.
func (ndb *NotifyingDB) NewBatch() scoria.Batch {
	return ndb.inner.NewBatch()
}

// CreateCF делегирует вызов внутренней БД.
func (ndb *NotifyingDB) CreateCF(name string) error {
	return ndb.inner.CreateCF(name)
}

// DropCF делегирует вызов внутренней БД.
func (ndb *NotifyingDB) DropCF(name string) error {
	return ndb.inner.DropCF(name)
}

// ListCFs делегирует вызов внутренней БД.
func (ndb *NotifyingDB) ListCFs() []string {
	return ndb.inner.ListCFs()
}

// Close делегирует вызов внутренней БД.
func (ndb *NotifyingDB) Close() error {
	return ndb.inner.Close()
}