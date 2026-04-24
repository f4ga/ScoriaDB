package scoria

import (
	"math"
	"sync/atomic"

	"scoriadb/internal/cf"
)

// CFDB расширяет интерфейс DB поддержкой Column Families.
type CFDB interface {
	DB

	// GetCF возвращает значение по ключу из указанного Column Family.
	GetCF(cf string, key []byte) ([]byte, error)
	// PutCF записывает значение по ключу в указанное Column Family.
	PutCF(cf string, key, value []byte) error
	// DeleteCF удаляет ключ из указанного Column Family.
	DeleteCF(cf string, key []byte) error
	// CreateCF создаёт новое Column Family.
	CreateCF(name string) error
	// DropCF удаляет Column Family.
	DropCF(name string) error
	// ListCFs возвращает список имён всех Column Families.
	ListCFs() []string
}

// ScoriaDB реализация CFDB с использованием реестра Column Families.
type ScoriaDB struct {
	registry *cf.Registry
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

// Close закрывает все Column Families и освобождает ресурсы.
func (db *ScoriaDB) Close() error {
	return db.registry.Close()
}

// EmbeddedCFDB возвращает интерфейс CFDB для встраивания.
func EmbeddedCFDB(dataDir string) (CFDB, error) {
	return NewScoriaDB(dataDir)
}