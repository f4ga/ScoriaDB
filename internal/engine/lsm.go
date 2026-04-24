package engine

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sync"
	"scoriadb/internal/engine/sstable"
	"scoriadb/internal/engine/vfs"
	"scoriadb/internal/mvcc"
)

// LSMEngine представляет LSM-движок с VLog и MVCC.
type LSMEngine struct {
	mu        sync.RWMutex
	dataDir   string
	memTable  *MemTable
	vlog      *VLog
	wal       *WAL
	manifest  *Manifest           // журнал метаданных SSTable
	vfs       vfs.VFS             // абстракция файловой системы
	levels    [][]*sstable.Reader // уровни SSTable (Level0, Level1, ...)
	LastTS    uint64              // атомарный счетчик timestamp
	closed    bool
	memSize   int64               // приблизительный размер MemTable в байтах
}

// NewLSMEngine создает новый LSM-движок.
func NewLSMEngine(dataDir string) (*LSMEngine, error) {
	// Создаём VFS (стандартная реализация, использующая os)
	vfs := vfs.NewDefaultVFS()
	
	// Создаём директорию через VFS
	if err := vfs.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Открываем манифест
	manifestPath := filepath.Join(dataDir, "MANIFEST")
	manifest, err := NewManifest(vfs, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest: %w", err)
	}

	// Открываем VLog (пока используем старый API, позже обновим)
	vlogPath := filepath.Join(dataDir, "vlog.db")
	vlog, err := OpenVLog(vlogPath)
	if err != nil {
		manifest.Close()
		return nil, fmt.Errorf("failed to open vlog: %w", err)
	}

	// Открываем WAL (пока используем старый API)
	walPath := filepath.Join(dataDir, "wal.log")
	wal, err := OpenWAL(walPath)
	if err != nil {
		vlog.Close()
		manifest.Close()
		return nil, fmt.Errorf("failed to open wal: %w", err)
	}

	// Восстанавливаем данные из WAL
	memTable := NewMemTable()
	if err := recoverFromWAL(wal, memTable, vlog); err != nil {
		vlog.Close()
		wal.Close()
		manifest.Close()
		return nil, fmt.Errorf("failed to recover from wal: %w", err)
	}

	// Определяем последний timestamp (пока простой счетчик)
	lastTS := uint64(1) // TODO: восстановить из данных

	// Загружаем существующие SSTable из манифеста
	levels := make([][]*sstable.Reader, 10) // предполагаем максимум 10 уровней
	manifestLevels := manifest.GetLevels()
	for level, infos := range manifestLevels {
		if level >= len(levels) {
			continue
		}
		for _, info := range infos {
			// Формируем путь к SSTable файлу
			sstPath := filepath.Join(dataDir, fmt.Sprintf("%06d.sst", info.FileNum))
			reader, err := sstable.Open(sstPath)
			if err != nil {
				// Если файл отсутствует, игнорируем (возможно, был удалён)
				continue
			}
			levels[level] = append(levels[level], reader)
		}
	}

	engine := &LSMEngine{
		dataDir:  dataDir,
		memTable: memTable,
		vlog:     vlog,
		wal:      wal,
		manifest: manifest,
		vfs:      vfs,
		levels:   levels,
		LastTS:   lastTS,
		memSize:  0,
	}
	return engine, nil
}

// PutWithTS записывает ключ-значение с указанным timestamp.
func (e *LSMEngine) PutWithTS(key, value []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	// Определяем, нужно ли писать в VLog
	var vp ValuePointer
	var inlineValue []byte
	if len(value) <= MaxInlineSize {
		inlineValue = value
	} else {
		var err error
		vp, err = e.vlog.Write(value)
		if err != nil {
			return fmt.Errorf("failed to write to vlog: %w", err)
		}
	}

	// Создаем MVCCKey
	mvccKey := mvcc.NewMVCCKey(key, commitTS)

	// Подготавливаем значение для MemTable
	var storedValue []byte
	if vp.Size > 0 {
		// Сериализуем ValuePointer
		storedValue = encodeValuePointer(vp)
	} else {
		storedValue = inlineValue
	}

	// Записываем в WAL
	walEntry := &WalEntry{
		Op:        OpPut,
		Key:       key,
		Value:     storedValue, // храним либо inline значение, либо указатель
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}

	// Обновляем MemTable
	e.memTable.Put(mvccKey, storedValue)

	// TODO: проверка необходимости flush
	return nil
}

// GetWithTS возвращает значение для ключа на момент snapshotTS.
func (e *LSMEngine) GetWithTS(key []byte, snapshotTS uint64) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, fmt.Errorf("engine closed")
	}

	// Ищем в MemTable
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)
	val, found := e.memTable.Get(mvccKey)
	if found {
		return e.decodeStoredValue(val)
	}

	// Ищем в SSTable (по уровням)
	for _, level := range e.levels {
		for _, sst := range level {
			if val, found := sst.Lookup(mvccKey); found {
				return e.decodeStoredValue(val)
			}
		}
	}

	return nil, nil // ключ не найден
}

// DeleteWithTS помечает ключ как удаленный (tombstone).
func (e *LSMEngine) DeleteWithTS(key []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	// Записываем в WAL
	walEntry := &WalEntry{
		Op:        OpDelete,
		Key:       key,
		Value:     nil,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}

	// Вставляем tombstone (значение nil) в MemTable
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	e.memTable.DeleteWithTS(mvccKey)

	return nil
}

// Close освобождает ресурсы движка.
func (e *LSMEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	var errs []error
	// Закрываем SSTable readers
	for _, level := range e.levels {
		for _, reader := range level {
			if err := reader.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := e.vlog.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := e.wal.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := e.manifest.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors while closing engine: %v", errs)
	}
	return nil
}

// decodeStoredValue преобразует хранимое значение (inline или ValuePointer) в исходное значение.
func (e *LSMEngine) decodeStoredValue(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		// tombstone
		return nil, nil
	}
	// Пытаемся декодировать как ValuePointer
	if vp, ok := decodeValuePointer(stored); ok {
		// Читаем из VLog
		return e.vlog.Read(vp)
	}
	// Иначе это inline значение
	return stored, nil
}

// encodeValuePointer сериализует ValuePointer в байты.
func encodeValuePointer(vp ValuePointer) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(vp.Size))
	return buf
}

// decodeValuePointer десериализует ValuePointer из байтов.
func decodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != 12 {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:12])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}

// recoverFromWAL восстанавливает MemTable из WAL.
func recoverFromWAL(wal *WAL, memTable *MemTable, vlog *VLog) error {
	return wal.Recover(func(entry *WalEntry) error {
		mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
		switch entry.Op {
		case OpPut:
			memTable.Put(mvccKey, entry.Value)
		case OpDelete:
			memTable.Put(mvccKey, nil)
		}
		return nil
	})
}
