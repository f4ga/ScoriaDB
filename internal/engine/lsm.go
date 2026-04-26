package engine

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"scoriadb/internal/engine/sstable"
	"scoriadb/internal/engine/vfs"
	"scoriadb/internal/mvcc"
)

// LSMEngine represents an LSM engine with VLog and MVCC.
type LSMEngine struct {
	mu             sync.RWMutex
	dataDir        string
	memTable       *MemTable
	frozenMemTable *MemTable
	vlog           *VLog
	wal            *WAL
	manifest       *Manifest           // SSTable metadata journal
	vfs            vfs.VFS             // filesystem abstraction
	levels         [][]*sstable.Reader // SSTable levels (Level0, Level1, ...)
	LastTS         uint64              // atomic timestamp counter
	closed         bool
	memSize        int64               // approximate MemTable size in bytes
}

// NewLSMEngine creates a new LSM engine.
func NewLSMEngine(dataDir string) (*LSMEngine, error) {
	// Create VFS (standard implementation using os)
	vfs := vfs.NewDefaultVFS()
	
	// Create directory via VFS
	if err := vfs.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Open manifest
	manifestPath := filepath.Join(dataDir, "MANIFEST")
	manifest, err := NewManifest(vfs, manifestPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open manifest: %w", err)
	}

	// Open VLog (currently using old API, will be updated later)
	vlogPath := filepath.Join(dataDir, "vlog.db")
	vlog, err := OpenVLog(vlogPath)
	if err != nil {
		manifest.Close()
		return nil, fmt.Errorf("failed to open vlog: %w", err)
	}

	// Open WAL (currently using old API)
	walPath := filepath.Join(dataDir, "wal.log")
	wal, err := OpenWAL(walPath)
	if err != nil {
		vlog.Close()
		manifest.Close()
		return nil, fmt.Errorf("failed to open wal: %w", err)
	}

	// Recover data from WAL
	memTable := NewMemTable()
	if err := recoverFromWAL(wal, memTable, vlog); err != nil {
		vlog.Close()
		wal.Close()
		manifest.Close()
		return nil, fmt.Errorf("failed to recover from wal: %w", err)
	}

	// Determine last timestamp (simple counter for now)
	lastTS := uint64(1) // TODO: restore from persisted data

	// Load existing SSTables from manifest
	levels := make([][]*sstable.Reader, 10) // assume at most 10 levels
	manifestLevels := manifest.GetLevels()
	for level, infos := range manifestLevels {
		if level >= len(levels) {
			continue
		}
		for _, info := range infos {
			// Build SSTable file path
			sstPath := filepath.Join(dataDir, fmt.Sprintf("%06d.sst", info.FileNum))
			reader, err := sstable.Open(sstPath)
			if err != nil {
				// If file missing, ignore (maybe deleted)
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

// NextTimestamp returns the next unique timestamp (atomically increments LastTS).
func (e *LSMEngine) NextTimestamp() uint64 {
	// Use atomic operation to increment LastTS
	return atomic.AddUint64(&e.LastTS, 1)
}

// PutWithTS writes a key‑value pair with the given timestamp.
func (e *LSMEngine) PutWithTS(key, value []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	// Decide whether to write to VLog
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

	// Create MVCCKey
	mvccKey := mvcc.NewMVCCKey(key, commitTS)

	// Prepare value for MemTable
	var storedValue []byte
	if vp.Size > 0 {
		// Serialize ValuePointer
		storedValue = encodeValuePointer(vp)
	} else {
		storedValue = inlineValue
	}

	// Write to WAL
	walEntry := &WalEntry{
		Op:        OpPut,
		Key:       key,
		Value:     storedValue, // either inline value or pointer
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}

	// Update MemTable
	e.memTable.Put(mvccKey, storedValue)

	// TODO: check if flush is needed
	return nil
}

// GetWithTS returns the value for a key as of snapshotTS.
func (e *LSMEngine) GetWithTS(key []byte, snapshotTS uint64) ([]byte, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.closed {
		return nil, fmt.Errorf("engine closed")
	}

	// Search in MemTable
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)
	val, found := e.memTable.Get(mvccKey)
	if found {
		return e.decodeStoredValue(val)
	}

	// Search in SSTables (by levels)
	for _, level := range e.levels {
		for _, sst := range level {
			if val, found := sst.Lookup(mvccKey); found {
				return e.decodeStoredValue(val)
			}
		}
	}

	return nil, nil // key not found
}

// DeleteWithTS marks a key as deleted (tombstone).
func (e *LSMEngine) DeleteWithTS(key []byte, commitTS uint64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return fmt.Errorf("engine closed")
	}

	// Write to WAL
	walEntry := &WalEntry{
		Op:        OpDelete,
		Key:       key,
		Value:     nil,
		Timestamp: commitTS,
	}
	if err := e.wal.Write(walEntry); err != nil {
		return fmt.Errorf("failed to write to wal: %w", err)
	}

	// Insert tombstone (nil value) into MemTable
	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	e.memTable.DeleteWithTS(mvccKey)

	return nil
}


// ActiveMemTable returns the active (current) MemTable.
func (e *LSMEngine) ActiveMemTable() *MemTable {
	return e.memTable
}

// FrozenMemTable returns the frozen MemTable (if any).
func (e *LSMEngine) FrozenMemTable() *MemTable {
	return e.frozenMemTable
}

// Close releases engine resources.
func (e *LSMEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.closed {
		return nil
	}
	e.closed = true

	var errs []error
	// Close SSTable readers
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

// decodeStoredValue converts stored value (inline or ValuePointer) back to original value.
func (e *LSMEngine) decodeStoredValue(stored []byte) ([]byte, error) {
	if len(stored) == 0 {
		// tombstone
		return nil, nil
	}
	// Try to decode as ValuePointer
	if vp, ok := decodeValuePointer(stored); ok {
		// Read from VLog
		return e.vlog.Read(vp)
	}
	// Otherwise it's an inline value
	return stored, nil
}

// encodeValuePointer serializes a ValuePointer to bytes.
func encodeValuePointer(vp ValuePointer) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint64(buf[0:8], uint64(vp.Offset))
	binary.BigEndian.PutUint32(buf[8:12], uint32(vp.Size))
	return buf
}

// decodeValuePointer deserializes a ValuePointer from bytes.
func decodeValuePointer(data []byte) (ValuePointer, bool) {
	if len(data) != 12 {
		return ValuePointer{}, false
	}
	offset := binary.BigEndian.Uint64(data[0:8])
	size := binary.BigEndian.Uint32(data[8:12])
	return ValuePointer{Offset: int64(offset), Size: int32(size)}, true
}

// ReadVLogValue reads a value from the Value Log by pointer.
// Pointer format: [fileID: 8 bytes][offset: 4 bytes].
// In the current implementation fileID is ignored (always 0) and offset is interpreted as size.
func (e *LSMEngine) ReadVLogValue(fileID uint64, offset uint32) ([]byte, error) {
	if e.vlog == nil {
		return nil, fmt.Errorf("vlog not initialized")
	}
	// Create ValuePointer: fileID becomes offset, offset becomes size
	vp := ValuePointer{Offset: int64(fileID), Size: int32(offset)}
	return e.vlog.Read(vp)
}

// recoverFromWAL recovers MemTable from WAL.
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
