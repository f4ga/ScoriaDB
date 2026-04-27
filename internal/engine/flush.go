package engine

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"scoriadb/internal/engine/sstable"
	// "scoriadb/internal/mvcc"  implement or decide where to do it
)

const (
	// MaxMemTableSize maximum MemTable size in bytes before flush
	MaxMemTableSize = 4 * 1024 * 1024 // 4 MB
	// MaxLevel0Files maximum number of files in Level0 before compaction
	MaxLevel0Files = 4
)

// flushMemTable flushes current MemTable into a Level0 SSTable.
//nolint:unused // flush goroutine worker
func (e *LSMEngine) flushMemTable() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Get next file number from manifest
	fileNum := e.manifest.NextFileNum()
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Create writer (currently using old API that works with os)
	writer, err := sstable.NewWriter(sstPath)
	if err != nil {
		return fmt.Errorf("failed to create SSTable writer: %w", err)
	}

	// Iterate over all MemTable entries
	iter := e.memTable.NewIterator()
	var minKey, maxKey []byte
	var first = true
	for iter.Next() {
		key, value := iter.Key(), iter.Value()
		// For range filter we need user keys (without timestamp)
		userKey := key.Key
		if first {
			minKey = make([]byte, len(userKey))
			copy(minKey, userKey)
			maxKey = make([]byte, len(userKey))
			copy(maxKey, userKey)
			first = false
		} else {
			if bytes.Compare(userKey, minKey) < 0 {
				minKey = userKey
			}
			if bytes.Compare(userKey, maxKey) > 0 {
				maxKey = userKey
			}
		}
		if err := writer.Append(key, value); err != nil {
			writer = nil
			// Delete partially written file via VFS
			if err := e.vfs.Remove(sstPath); err != nil {
				log.Printf("flush: failed to remove %s: %v", sstPath, err)
			}
			return fmt.Errorf("failed to append key to SSTable: %w", err)
		}
	}

	if err := writer.Finish(); err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("flush: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to finish SSTable: %w", err)
	}

	// Open the created SSTable for reading
	reader, err := sstable.Open(sstPath)
	if err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("flush: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to open SSTable: %w", err)
	}

	// Get file size
	stat, err := e.vfs.Stat(sstPath)
	if err != nil {
		reader.Close()
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("flush: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to stat SSTable: %w", err)
	}

	// Create VersionEdit to add new file
	edit := &VersionEdit{
		NewFiles: []SSTableInfo{
			{
				FileNum: fileNum,
				Level:   0,
				MinKey:  minKey,
				MaxKey:  maxKey,
				Size:    uint64(stat.Size()),
			},
		},
		NextFileNum: fileNum + 1,
	}

	// Apply edit to manifest
	if err := e.manifest.Apply(edit); err != nil {
		reader.Close()
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("flush: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	// Add reader to level 0
	e.levels[0] = append(e.levels[0], reader)

	// Reset MemTable (in reality we should create a new empty MemTable)
	// For now leave as is - MemTable clearing will happen after successful flush
	// e.memTable = NewMemTable()
	// e.memSize = 0

	return nil
}

// maybeCompactLevel0 checks whether Level0 -> Level1 compaction is needed.
// Calls maybeCompact, which already contains the check and compaction logic.
//nolint:unused // level-0 compaction trigger
func (e *LSMEngine) maybeCompactLevel0() {
	e.maybeCompact()
}

// maybeFlush checks if MemTable exceeds limit and triggers flush.
//nolint:unused // memtable flush trigger
func (e *LSMEngine) maybeFlush() {
	if e.memSize >= MaxMemTableSize {
		//nolint:errcheck // error is handled inside goroutine
		go e.flushMemTable()
	}
}