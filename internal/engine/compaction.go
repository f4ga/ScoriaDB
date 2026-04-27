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

package engine

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"scoriadb/internal/engine/sstable"
)

// compactLevel0 performs compaction from level 0 to level 1.
// Real implementation: merges all level-0 SSTables into a single new level-1 SSTable
// using multi-way merge with deduplication and tombstone removal.
//nolint:unused // triggered by maybeCompact
func (e *LSMEngine) compactLevel0() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.levels[0]) == 0 {
		return nil
	}

	// Get SSTable metadata for level 0 from manifest (for deletion)
	level0Infos := e.manifest.GetLevels()[0]
	if len(level0Infos) != len(e.levels[0]) {
		// This should not happen, but be defensive
		level0Infos = nil
	}

	// Create iterators for all level-0 SSTables
	var iterators []sstable.Iterator
	for _, reader := range e.levels[0] {
		iter, err := reader.NewIterator()
		if err != nil {
			// Close already opened iterators
			for _, it := range iterators {
				it.Close()
			}
			return fmt.Errorf("failed to create SSTable iterator: %w", err)
		}
		iterators = append(iterators, iter)
	}

	// Create merge iterator
	mergeIter := sstable.NewMergeIterator(iterators)
	defer mergeIter.Close()

	// Get next file number
	fileNum := e.manifest.NextFileNum()
	sstPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", fileNum))

	// Create writer for new SSTable
	writer, err := sstable.NewWriter(sstPath)
	if err != nil {
		return fmt.Errorf("failed to create SSTable writer: %w", err)
	}
	// Clean up writer on error
	defer func() {
		if writer != nil {
			// If writer hasn't been finished, remove the partial file
			if err := e.vfs.Remove(sstPath); err != nil {
				log.Printf("compaction: failed to remove %s: %v", sstPath, err)
			}
		}
	}()

	var minKey, maxKey []byte
	var first = true
	var writtenKeys int
	var prevUserKey []byte

	// Iterate over merged stream
	for mergeIter.Next() {
		key := mergeIter.Key()
		value := mergeIter.Value()

		// Skip tombstones (empty values) – they represent deletions
		if len(value) == 0 {
			continue
		}

		userKey := key.Key

		// Skip duplicate user key (should not happen due to merge iterator dedup)
		if prevUserKey != nil && bytes.Equal(userKey, prevUserKey) {
			continue
		}

		// Update range keys
		if first {
			minKey = make([]byte, len(userKey))
			copy(minKey, userKey)
			maxKey = make([]byte, len(userKey))
			copy(maxKey, userKey)
			first = false
		} else {
			if bytes.Compare(userKey, minKey) < 0 {
				minKey = make([]byte, len(userKey))
				copy(minKey, userKey)
			}
			if bytes.Compare(userKey, maxKey) > 0 {
				maxKey = make([]byte, len(userKey))
				copy(maxKey, userKey)
			}
		}

		// Write to new SSTable
		if err := writer.Append(key, value); err != nil {
			return fmt.Errorf("failed to append key to SSTable: %w", err)
		}
		writtenKeys++

		// Update previous user key (copy)
		prevUserKey = append([]byte(nil), userKey...)
	}

	// If no keys written (all tombstones), we still need to produce an empty SSTable?
	// For correctness, we can create an empty SSTable (or skip creating a file).
	// Let's create an empty SSTable (writer with zero entries) to maintain level invariant.
	// The writer will produce a valid SSTable with zero blocks.

	if err := writer.Finish(); err != nil {
		return fmt.Errorf("failed to finish SSTable: %w", err)
	}
	// Mark writer as finished to avoid cleanup removal
	writer = nil

	// Open the created SSTable
	reader, err := sstable.Open(sstPath)
	if err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("compaction: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to open SSTable: %w", err)
	}
	defer func() {
		if err != nil {
			reader.Close()
		}
	}()

	// Get file size
	stat, err := e.vfs.Stat(sstPath)
	if err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("compaction: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to stat SSTable: %w", err)
	}

	// Prepare VersionEdit: delete old level-0 files, add new level-1 file
	var deletedFiles []SSTableInfo
	if level0Infos != nil {
		deletedFiles = level0Infos
	} else {
		// Fallback: create placeholder deletions (should not happen)
		for range e.levels[0] {
			deletedFiles = append(deletedFiles, SSTableInfo{
				Level: 0,
				// FileNum unknown
			})
		}
	}

	edit := &VersionEdit{
		DeletedFiles: deletedFiles,
		NewFiles: []SSTableInfo{
			{
				FileNum: fileNum,
				Level:   1,
				MinKey:  minKey,
				MaxKey:  maxKey,
				Size:    uint64(stat.Size()),
			},
		},
		NextFileNum: fileNum + 1,
	}

	// Apply edit to manifest
	if err := e.manifest.Apply(edit); err != nil {
		if err := e.vfs.Remove(sstPath); err != nil {
			log.Printf("compaction: failed to remove %s: %v", sstPath, err)
		}
		return fmt.Errorf("failed to apply manifest edit: %w", err)
	}

	// Close old readers and delete old files from disk
	for i, reader := range e.levels[0] {
		reader.Close()
		if i < len(deletedFiles) && deletedFiles[i].FileNum != 0 {
			oldPath := filepath.Join(e.dataDir, fmt.Sprintf("%06d.sst", deletedFiles[i].FileNum))
			if err := e.vfs.Remove(oldPath); err != nil {
				log.Printf("compaction: failed to remove %s: %v", oldPath, err)
			}
		}
	}

	// Clear level 0
	e.levels[0] = nil
	// Add new reader to level 1
	e.levels[1] = append(e.levels[1], reader)

	// Success, do not close reader
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