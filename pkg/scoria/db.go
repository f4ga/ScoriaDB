package scoria

import (
	"math"
	"sync/atomic"
	"scoriadb/internal/engine"
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