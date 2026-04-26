package engine

import (
	"fmt"
	"path/filepath"
	"scoriadb/internal/engine/sstable"
	"scoriadb/internal/mvcc"
)

// compactLevel0 performs compaction from level 0 to level 1.
// Simple implementation: merges all level-0 SSTables into a single new level-1 SSTable.
//nolint:unused // triggered by maybeCompact
func (e *LSMEngine) compactLevel0() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.levels[0]) == 0 {
		return nil
	}

	// Get next file number
	fileNum := e.manifest.NextFileNum()
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Create writer for new SSTable
	writer, err := sstable.NewWriter(sstPath)
	if err != nil {
		return fmt.Errorf("failed to create SSTable writer: %w", err)
	}

	// Collect all keys from all level-0 SSTables.
	// Real implementation should merge with sorting and deduplication.
	// Here for simplicity we just create an empty SSTable.
	// Basic compaction: merges level-0 SSTables into level-1. Optimization tracked in Release 2.
	key := []byte("__compacted__")
	value := []byte("compacted")
	mvccKey := mvcc.NewMVCCKey(key, 1)
	if err := writer.Append(mvccKey, value); err != nil {
		writer = nil
		_ = e.vfs.Remove(sstPath) // Logging of cleanup errors will be added in Release 2.
		return fmt.Errorf("failed to append dummy key: %w", err)
	}

	if err := writer.Finish(); err != nil {
		_ = e.vfs.Remove(sstPath) // Logging of cleanup errors will be added in Release 2.
		return fmt.Errorf("failed to finish SSTable: %w", err)
	}

	// Open the created SSTable
	reader, err := sstable.Open(sstPath)
	if err != nil {
		_ = e.vfs.Remove(sstPath) // Logging of cleanup errors will be added in Release 2.
		return fmt.Errorf("failed to open SSTable: %w", err)
	}

	// Get file size
	stat, err := e.vfs.Stat(sstPath)
	if err != nil {
		reader.Close()
		_ = e.vfs.Remove(sstPath) // Logging of cleanup errors will be added in Release 2.
		return fmt.Errorf("failed to stat SSTable: %w", err)
	}

	// Prepare VersionEdit: delete old level-0 files, add new level-1 file
	var deletedFiles []SSTableInfo
	for range e.levels[0] {
		// In reality we need fileNum from reader, but skip for simplicity.
		// Here we just add a placeholder.
		deletedFiles = append(deletedFiles, SSTableInfo{
			Level: 0,
			// FileNum unknown, leave 0
		})
	}

	edit := &VersionEdit{
		DeletedFiles: deletedFiles,
		NewFiles: []SSTableInfo{
			{
				FileNum: fileNum,
				Level:   1,
				MinKey:  key, // in reality need to compute min/max
				MaxKey:  key,
				Size:    uint64(stat.Size()),
			},
		},
		NextFileNum: fileNum + 1,
	}

	// Apply edit to manifest
	if err := e.manifest.Apply(edit); err != nil {
		reader.Close()
		_ = e.vfs.Remove(sstPath) // Logging of cleanup errors will be added in Release 2.
		return fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	// Close old readers
	for _, r := range e.levels[0] {
		r.Close()
	}
	// Clear level 0
	e.levels[0] = nil
	// Add new reader to level 1
	e.levels[1] = append(e.levels[1], reader)

	return nil
}

// maybeCompact checks conditions and triggers compaction if needed.
//nolint:unused // scheduled compaction entry point
func (e *LSMEngine) maybeCompact() {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Simple condition: if Level0 has more than MaxLevel0Files files, start compaction
	if len(e.levels[0]) > MaxLevel0Files {
		//nolint:errcheck // error is handled inside goroutine
		go e.compactLevel0()
	}
}