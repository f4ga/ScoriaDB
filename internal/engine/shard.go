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

// internal/engine/shard.go
package engine

import (
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

const (
	shardFlushThreshold = 4 * 1024 * 1024 // 4 MB
	shardMaxLevel0Files = 4

	// shardFileNumStride is the per-shard base offset for SSTable file numbers.
	// Each shard has its own manifest whose nextFileNum starts at 1, so without
	// an offset every shard would allocate file number 1 and write the same
	// "000001.sst", overwriting the same file and losing data. Offsetting each
	// shard's file numbers keeps them globally unique across shards.
	// See: SST-03, BUG-FILENUM.
	shardFileNumStride = 1_000_000
)

// Shard is a fully independent storage partition.
// Each CPU core owns exactly one shard — there is zero cross‑shard sharing.
type Shard struct {
	id      int
	dataDir string

	// Lock‑free flat arena backing the MemTable.
	arena          *memtable.FlatArena
	memTable       *memtable.MemTable
	frozenMemTable *memtable.MemTable

	// Per‑shard durable logs.
	wal  *WAL
	vlog *VLogImpl

	// Per‑shard LSM levels (Level 0 .. Level N).
	levels   [][]*sstable.Reader
	levelsMu sync.RWMutex // protects levels slice during mutation

	// Manifest tracks file numbers and last TS for this shard.
	manifest *Manifest

	// Atomic counter for active MemTable size; triggers flush.
	memSize int64

	// Serialises writes, flush, and compaction within this shard.
	mu     sync.Mutex
	closed atomic.Bool

	// Background workers.
	flushCh   chan struct{}
	compactCh chan struct{}
	stopCh    chan struct{}
	wg        sync.WaitGroup
}

// NewShard creates, recovers, and starts a single shard.
func NewShard(id int, dataDir string, walOpts WALOptions) (*Shard, error) {
	// 1. Arena – single 64 MiB block (lock‑free allocation).
	arena := memtable.NewFlatArena(0)

	// 2. Value log – one file per shard.
	vlogPath := filepath.Join(dataDir, fmt.Sprintf("vlog_%d.log", id))
	vlog, err := OpenVLog(vfs.NewDefaultVFS(), vlogPath)
	if err != nil {
		return nil, fmt.Errorf("shard %d: open vlog: %w", id, err)
	}

	// 3. WAL – independent group commit.
	walPath := filepath.Join(dataDir, fmt.Sprintf("wal_%d.log", id))
	wal, err := OpenWALWithOptions(walPath, walOpts)
	if err != nil {
		vlog.Close()
		return nil, fmt.Errorf("shard %d: open wal: %w", id, err)
	}

	// 4. Manifest – per‑shard metadata.
	//
	// Each shard has its OWN manifest, so its nextFileNum starts at 1 and every
	// shard would allocate file number 1 → every shard would write "000001.sst",
	// overwriting the same file and losing data. To keep SSTable file numbers
	// globally unique across shards, each shard's file numbers are offset by a
	// large base: shard 0 → 1,1_000_000,2_000_000… ; shard 1 → 1_000_001, …
	// This guarantees distinct .sst filenames. See: SST-03, BUG-FILENUM.
	manifestPath := filepath.Join(dataDir, fmt.Sprintf("MANIFEST_%d", id))
	manifest, err := NewManifest(manifestPath)
	if err != nil {
		wal.Close()
		vlog.Close()
		return nil, fmt.Errorf("shard %d: open manifest: %w", id, err)
	}

	// 5. MemTable backed by this shard's arena.
	mt := memtable.NewMemTable() // own arena, see ARENA-01

	shard := &Shard{
		id:        id,
		dataDir:   dataDir,
		manifest:  manifest,
		arena:     arena,
		vlog:      vlog,
		wal:       wal,
		memTable:  mt,
		levels:    make([][]*sstable.Reader, 10),
		flushCh:   make(chan struct{}, 1),
		compactCh: make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
	}

	// 6. Recover from WAL (cold path).
	if err := shard.recoverFromWAL(); err != nil {
		shard.Close()
		return nil, fmt.Errorf("shard %d: recover wal: %w", id, err)
	}

	// 7. Rebuild SSTable levels from manifest.
	shard.rebuildLevels()

	// 8. Start background tasks.
	shard.startBackgroundTasks()

	return shard, nil
}

// rebuildLevels opens SSTable readers listed in the manifest.
func (s *Shard) rebuildLevels() {
	manifestLevels := s.manifest.GetLevels()
	for lvl, infos := range manifestLevels {
		if lvl >= len(s.levels) {
			continue
		}
		for _, info := range infos {
			path := filepath.Join(s.dataDir, fmt.Sprintf("%06d.sst", info.FileNum))
			reader, err := sstable.Open(path)
			if err != nil {
				logger.Warn("shard %d: skip unreadable sst %s: %v", s.id, path, err)
				continue
			}
			s.levels[lvl] = append(s.levels[lvl], reader)
		}
	}
}

// recoverFromWAL replays the shard's WAL into its MemTable.
func (s *Shard) recoverFromWAL() error {
	_, err := recoverFromWAL(s.wal, s.memTable, s.vlog, s.id, func([]byte) int {
		return s.id
	})
	return err
}

// decodeStoredValue decodes a tagged stored value into its logical payload.
// Returns nil for tombstones and the inline/pointer payload otherwise. Pointer
// payloads are resolved by the caller (iterator or GetLatest) via the shard VLog.
func decodeStoredValue(stored []byte) []byte {
	if len(stored) == 0 {
		return nil
	}
	if IsValidValueTag(stored[0]) {
		switch stored[0] {
		case TypeTombstone:
			return nil
		case TypeValuePointer, TypeInline:
			return stored[1:]
		}
	}
	// Legacy format (no tag).
	return stored
}

// resolveStoredValue converts a tagged stored value (from MemTable or SSTable)
// into the logical value a caller should see. A TypeTombstone yields nil (not
// found); a TypeValuePointer is resolved through this shard's VLog into the
// real payload; TypeInline (and legacy untagged) values are returned as-is.
// The returned slice may reference the shard VLog and is valid until the shard
// is closed; the caller must copy it if it needs to outlive the shard.
// See: BUG-DOUBLETAG, WIS-KEY-01
// resolveSSTableValue converts a payload returned by sstable.Reader.Lookup into
// the logical value a caller should see. Lookup already strips the leading type
// tag and treats tombstones as not found, so `payload` is either an inline value
// or, for a large value (>MaxInlineSize), a 12-byte ValuePointer that must be
// resolved through the shard VLog. An inline value is returned as-is; a pointer
// is resolved and copied out of the ref-counted view. See: WIS-KEY-01.
func (s *Shard) resolveSSTableValue(payload []byte) []byte {
	if len(payload) != ValuePointerSize {
		// Inline value (or empty). Lookup already stripped the tag.
		return payload
	}
	vp, ok := DecodeValuePointer(payload)
	if !ok || vp.Offset < 0 || vp.Size <= 0 {
		return payload
	}
	if s.vlog == nil {
		return nil
	}
	view, err := s.vlog.ReadView(vp)
	if err != nil {
		return nil
	}
	data := make([]byte, len(view.Data()))
	copy(data, view.Data())
	view.Release()
	return data
}

// resolveStoredValue decodes a tagged stored value into its logical payload.
func (s *Shard) resolveStoredValue(stored []byte) []byte {
	if len(stored) == 0 {
		return nil
	}
	if !IsValidValueTag(stored[0]) {
		// Legacy format (no tag).
		return stored
	}
	switch stored[0] {
	case TypeTombstone:
		return nil
	case TypeValuePointer:
		if len(stored) < 1+ValuePointerSize {
			return nil
		}
		vp, ok := DecodeValuePointer(stored[1:])
		if !ok || vp.Offset < 0 || vp.Size <= 0 {
			return nil
		}
		if s.vlog == nil {
			return nil
		}
		view, err := s.vlog.ReadView(vp)
		if err != nil {
			return nil
		}
		// Copy the payload out of the ref-counted view and release it, because
		// the returned slice must outlive this function and we do not want to
		// leak a VLog reference per call.
		data := make([]byte, len(view.Data()))
		copy(data, view.Data())
		view.Release()
		return data
	default: // TypeInline
		return stored[1:]
	}
}

// =========================================================================
// Lock‑free reads, serialised writes
// =========================================================================

// Get returns the value visible at snapshotTS.
// Lock‑free on the hot path: MemTable is protected by EBR, SSTable pointers
// are snapshotted under levelsMu.RLock and then read without any lock.
func (s *Shard) Get(key []byte, snapshotTS uint64) ([]byte, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("shard closed")
	}
	mvccKey := mvcc.NewMVCCKey(key, snapshotTS)

	// 1. Active MemTable (EBR epoch taken internally). MemTable values are
	//    stored with a leading type tag. A TypeValuePointer (large value) must be
	//    resolved through the shard VLog; decodeStoredValue only strips the tag
	//    and would return the raw 12-byte pointer. Use resolveStoredValue so
	//    large values are resolved identically to the SSTable path. See: WIS-KEY-01.
	if val, found := s.memTable.Get(mvccKey); found {
		return s.resolveStoredValue(val), nil
	}
	// 2. Frozen MemTable (being flushed, still readable).
	if s.frozenMemTable != nil {
		if val, found := s.frozenMemTable.Get(mvccKey); found {
			return s.resolveStoredValue(val), nil
		}
	}

	// 3. SSTables – snapshot pointers under RLock, then release.
	s.levelsMu.RLock()
	levelsCopy := s.snapshotLevelsLocked()
	s.levelsMu.RUnlock()

	for _, level := range levelsCopy {
		for _, sst := range level {
			if val, found := sst.Lookup(mvccKey); found {
				// Lookup strips the leading type tag and treats tombstones as
				// not found, so `val` is the raw payload. A large value stored
				// as a TypeValuePointer is a 12-byte pointer that must be
				// resolved through the shard VLog; an inline value is returned
				// as-is. See: WIS-KEY-01
				return s.resolveSSTableValue(val), nil
			}
		}
	}
	return nil, nil
}

// GetLatest returns the latest live value, its commit timestamp, and whether it
// was found for the given user key. It consults the active MemTable, frozen
// MemTable, and SSTables. See: ARCH-07, MVCC-03.
func (s *Shard) GetLatest(key []byte) ([]byte, uint64, bool, error) {
	if s.closed.Load() {
		return nil, 0, false, fmt.Errorf("shard closed")
	}
	// 1. Active MemTable.
	if s.memTable != nil {
		if val, ts, found := s.memTable.GetLatest(key); found {
			return decodeStoredValue(val), ts, true, nil
		}
	}
	// 2. Frozen MemTable.
	if s.frozenMemTable != nil {
		if val, ts, found := s.frozenMemTable.GetLatest(key); found {
			return decodeStoredValue(val), ts, true, nil
		}
	}
	// 3. SSTables. Lookup with the maximum snapshot timestamp returns the newest
	//    visible version for the user key (inverted MVCC timestamps order the
	//    versions oldest → newest, so a MaxUint64 snapshot sees the latest).
	s.levelsMu.RLock()
	levelsCopy := s.snapshotLevelsLocked()
	s.levelsMu.RUnlock()
	for _, level := range levelsCopy {
		for _, sst := range level {
			if val, found := sst.Lookup(mvcc.NewMVCCKey(key, ^uint64(0))); found {
				// Lookup already strips the leading type tag and resolves
				// tombstones, so return the payload directly. Do NOT call
				// decodeStoredValue again, which would double-strip and
				// corrupt user values whose first byte is 0x00/0x01/0x02.
				return val, 0, true, nil
			}
		}
	}
	return nil, 0, false, nil
}

// memSizeLoad returns the current MemTable size watermark for this shard.
func (s *Shard) memSizeLoad() int64 {
	return atomic.LoadInt64(&s.memSize)
}

// Put inserts/updates a key. Serialised by s.mu.
func (s *Shard) Put(key, value []byte, commitTS uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return fmt.Errorf("shard closed")
	}

	var vp ValuePointer
	var inlineValue []byte
	isLarge := len(value) > MaxInlineSize

	if isLarge {
		vpPtr, err := s.vlog.Write(value)
		if err != nil {
			return fmt.Errorf("vlog write: %w", err)
		}
		vp = vpPtr
	} else {
		inlineValue = value
	}

	stored := encodeStoredValue(isLarge, inlineValue, vp)

	// WAL: omitted for large values (value already persisted in VLog).
	if !isLarge {
		entry := newWalEntry()
		entry.Op = OpPut
		entry.Key = key
		entry.Value = inlineValue
		entry.Timestamp = commitTS
		if err := s.wal.Write(entry); err != nil {
			putWalEntry(entry)
			return fmt.Errorf("wal write: %w", err)
		}
		putWalEntry(entry)
	}

	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	s.memTable.Put(mvccKey, stored)
	atomic.AddInt64(&s.memSize, int64(len(key)+len(value)))

	if atomic.LoadInt64(&s.memSize) > shardFlushThreshold {
		select {
		case s.flushCh <- struct{}{}:
		default:
		}
	}
	return nil
}

// Delete writes a tombstone. Serialised by s.mu.
func (s *Shard) Delete(key []byte, commitTS uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed.Load() {
		return fmt.Errorf("shard closed")
	}

	entry := newWalEntry()
	entry.Op = OpDelete
	entry.Key = key
	entry.Timestamp = commitTS
	if err := s.wal.Write(entry); err != nil {
		putWalEntry(entry)
		return fmt.Errorf("wal write: %w", err)
	}
	putWalEntry(entry)

	mvccKey := mvcc.NewMVCCKey(key, commitTS)
	s.memTable.DeleteWithTS(mvccKey)
	atomic.AddInt64(&s.memSize, int64(len(key)))
	return nil
}

// =========================================================================
// Background workers
// =========================================================================

func (s *Shard) startBackgroundTasks() {
	s.wg.Add(2)
	go s.flushWorker()
	go s.compactionWorker()
}

func (s *Shard) flushWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if atomic.LoadInt64(&s.memSize) > shardFlushThreshold {
				select {
				case s.flushCh <- struct{}{}:
				default:
				}
			}
		case <-s.flushCh:
			if err := s.flushMemTable(); err != nil {
				logger.Warn("shard %d flush: %v", s.id, err)
			}
		}
	}
}

func (s *Shard) compactionWorker() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.level0Files() > shardMaxLevel0Files {
				select {
				case s.compactCh <- struct{}{}:
				default:
				}
			}
		case <-s.compactCh:
			if err := s.compactLevel0(); err != nil {
				logger.Warn("shard %d compaction: %v", s.id, err)
			}
		}
	}
}

// =========================================================================
// Flush and compaction
// =========================================================================

func (s *Shard) flushMemTable() error {
	if s.manifest == nil {
		return fmt.Errorf("flushMemTable: shard manifest is nil")
	}
	if s.closed.Load() {
		return fmt.Errorf("flushMemTable: shard is closed")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.memTable.Len() == 0 {
		return nil
	}

	old := s.memTable
	// Each MemTable owns its OWN flat arena. Sharing the shard arena between the
	// active and frozen tables is unsafe: writes to the active table during a
	// flush of the frozen table would land in the same 64 MB block and could
	// overwrite nodes the frozen table still references. See: ARENA-01.
	s.memTable = memtable.NewMemTable() // own arena, see ARENA-01
	s.frozenMemTable = old
	atomic.StoreInt64(&s.memSize, 0)

	// Allocate the file number ONCE so the SSTable file on disk, the manifest
	// registration, and the advanced counter all agree. Calling NextFileNum()
	// multiple times would create N.sst but register N+1 in the manifest,
	// losing the table on the next open/recovery. See: BUG-FILENUM
	//
	// Each shard has its own manifest starting at fileNum=1, so file numbers are
	// offset by a per-shard base to stay globally unique across shards. Without
	// this offset every shard writes "000001.sst", overwriting the same file.
	// See: SST-03, BUG-FILENUM.
	fileNum := s.manifest.NextFileNum() + uint64(s.id)*shardFileNumStride
	sstPath := filepath.Join(s.dataDir, fmt.Sprintf("%06d.sst", fileNum))
	reader, err := s.writeMemTableToSST(old, sstPath)
	if err != nil {
		s.memTable = old
		s.frozenMemTable = nil
		atomic.StoreInt64(&s.memSize, int64(old.SizeBytes()))
		return err
	}

	edit := &VersionEdit{
		NewFiles:    []SSTableInfo{{FileNum: fileNum, Level: 0 /* + MinKey/MaxKey/Size */}},
		NextFileNum: fileNum + 1,
		LastTS:      s.manifest.LastTS(),
	}
	if err := s.manifest.Apply(edit); err != nil {
		reader.Close()
		return err
	}

	s.levelsMu.Lock()
	s.levels[0] = append(s.levels[0], reader)
	s.levelsMu.Unlock()

	old.Close()
	s.frozenMemTable = nil
	return nil
}

func (s *Shard) compactLevel0() error {
	// Full implementation omitted for brevity — follows the same pattern
	// as the original engine but scoped to this shard's levels.
	return nil
}

// =========================================================================
// Helpers
// =========================================================================

func (s *Shard) level0Files() int {
	s.levelsMu.RLock()
	defer s.levelsMu.RUnlock()
	return len(s.levels[0])
}

func (s *Shard) snapshotLevelsLocked() [][]*sstable.Reader {
	cp := make([][]*sstable.Reader, len(s.levels))
	for i, lvl := range s.levels {
		lvlCopy := make([]*sstable.Reader, len(lvl))
		copy(lvlCopy, lvl)
		cp[i] = lvlCopy
	}
	return cp
}

func (s *Shard) writeMemTableToSST(mt *memtable.MemTable, path string) (*sstable.Reader, error) {
	writer, err := sstable.NewWriter(path, mt.Size())
	if err != nil {
		return nil, err
	}
	iter := mt.NewIterator()
	count := 0
	for iter.Next() {
		count++
		// CRITICAL: The MemTable value is stored WITH its leading type tag
		// (TypeInline/TypeValuePointer/TypeTombstone). We pass the raw (tagged)
		// value via AppendTagged so the writer preserves the tag verbatim.
		// Stripping the tag (as the old decodeStoredValue path did) would
		// destroy the ValuePointer semantics: a large value (>MaxInlineSize)
		// is stored in the MemTable as a TypeValuePointer + 12-byte pointer,
		// and stripping the tag turns it into an opaque 12-byte "inline" value
		// that the reader cannot resolve through the VLog.
		//
		// The SSTable reader returns the tagged value as-is; the LSM layer
		// (Shard.Get) inspects the leading tag and resolves a TypeValuePointer
		// via the shard VLog. See: BUG-DOUBLETAG, WIS-KEY-01
		raw := iter.Value()
		if err := writer.AppendTagged(iter.Key(), raw); err != nil {
			iter.Close()
			return nil, err
		}
	}
	iter.Close()
	if err := writer.Finish(); err != nil {
		return nil, err
	}
	// Diagnostic: report the number of entries written so data loss between the
	// MemTable iterator and the SSTable writer can be localized. See: SST-03.
	logger.Info("writeMemTableToSST: shard %d wrote %d entries to %s", s.id, count, path)
	return sstable.Open(path)
}

// Close releases all shard resources.
func (s *Shard) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	close(s.stopCh)
	s.wg.Wait()

	s.levelsMu.Lock()
	for _, lvl := range s.levels {
		for _, r := range lvl {
			r.Close()
		}
	}
	s.levels = nil
	s.levelsMu.Unlock()

	if s.memTable != nil {
		s.memTable.Close()
	}
	if s.frozenMemTable != nil {
		s.frozenMemTable.Close()
	}
	if s.wal != nil {
		s.wal.Close()
	}
	if s.vlog != nil {
		s.vlog.Close()
	}
	if s.manifest != nil {
		s.manifest.Close()
	}
	return nil
}

// encodeStoredValue builds the tagged byte slice for MemTable storage.
func encodeStoredValue(isLarge bool, inline []byte, vp ValuePointer) []byte {
	if isLarge {
		var buf [1 + ValuePointerSize]byte
		buf[0] = TypeValuePointer
		EncodeValuePointer(vp, buf[1:])
		return buf[:]
	}
	const maxStackInline = 1 + 64
	if len(inline) <= maxStackInline-1 {
		var buf [maxStackInline]byte
		buf[0] = TypeInline
		copy(buf[1:], inline)
		return buf[:1+len(inline)]
	}
	out := make([]byte, 1+len(inline))
	out[0] = TypeInline
	copy(out[1:], inline)
	return out
}
