// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/engine/sstable"
	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// TestDiagIterateFlushedSST directly opens each flushed SSTable and iterates ALL
// of its entries, printing them, to determine whether the data was actually
// written to disk during flush. See: PROMPT-SSTABLE-FINAL.
func TestDiagIterateFlushedSST(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("NewLSMEngine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	const n = 128
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		if err := eng.PutWithTS(key, []byte{byte(i)}, uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS(%d): %v", i, err)
		}
	}

	for _, shard := range eng.shards {
		if err := shard.flushMemTable(); err != nil {
			t.Fatalf("shard.flushMemTable: %v", err)
		}
	}

	// For each shard, list the .sst files and iterate them directly.
	total := 0
	matched := 0
	for idx, shard := range eng.shards {
		shard.levelsMu.RLock()
		readers := make([]*sstable.Reader, len(shard.levels[0]))
		copy(readers, shard.levels[0])
		shard.levelsMu.RUnlock()

		for _, r := range readers {
			it, err := r.NewIterator()
			if err != nil {
				t.Fatalf("shard %d NewIterator: %v", idx, err)
			}
			for it.Next() {
				total++
				k := it.Key()
				// The iterator strips the leading type tag from the value.
				v := it.Value()
				// Reconstruct the original user key for comparison.
				origIdx := int(k.Key[0]) | (int(k.Key[1]) << 8)
				// The value stored by PutWithTS is a single byte {byte(i)} tagged as
				// TypeInline (0x00). After the reader strips the tag, v is {byte(i)}.
				if origIdx >= 0 && origIdx < n && len(v) == 1 && v[0] == byte(origIdx) {
					matched++
				}
			}
			if err := it.Err(); err != nil {
				t.Fatalf("shard %d iterate: %v", idx, err)
			}
			it.Close()
			t.Logf("shard %d sst %s: footer.NumKeys=%d", idx, filepath.Base(r.Path()), r.NumKeys())
		}
	}
	t.Logf("TOTAL entries iterated=%d matched=%d", total, matched)

	// Also directly Lookup via the reader using the raw user key + inverted ts.
	foundViaLookup := 0
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		mk := mvcc.NewMVCCKey(key, uint64(i+1))
		for _, shard := range eng.shards {
			shard.levelsMu.RLock()
			readers := make([]*sstable.Reader, len(shard.levels[0]))
			copy(readers, shard.levels[0])
			shard.levelsMu.RUnlock()
			for _, r := range readers {
				// The value is stored with a leading TypeInline tag that the
				// reader strips, so the returned value is {byte(i)}.
				if v, ok := r.Lookup(mk); ok && len(v) == 1 && v[0] == byte(i) {
					foundViaLookup++
				}
			}
		}
	}
	t.Logf("found via direct Lookup=%d", foundViaLookup)

	if total != n {
		t.Errorf("iterated %d entries, want %d", total, n)
	}
	if matched != n {
		t.Errorf("matched %d, want %d", matched, n)
	}
	if foundViaLookup != n {
		t.Errorf("direct Lookup found %d, want %d", foundViaLookup, n)
	}
}

// TestDiagCompareVerification compares GetWithTS against the direct Lookup to
// find where the loss is introduced (Shard.Get resolution vs SSTable read).
func TestDiagCompareVerification(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("NewLSMEngine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	const n = 128
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		if err := eng.PutWithTS(key, []byte{byte(i)}, uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS(%d): %v", i, err)
		}
	}
	for _, shard := range eng.shards {
		if err := shard.flushMemTable(); err != nil {
			t.Fatalf("shard.flushMemTable: %v", err)
		}
	}

	// Dump every entry across all SSTables into a map keyed by string(key).
	inSST := make(map[string]bool)
	for _, shard := range eng.shards {
		shard.levelsMu.RLock()
		readers := make([]*sstable.Reader, len(shard.levels[0]))
		copy(readers, shard.levels[0])
		shard.levelsMu.RUnlock()
		for _, r := range readers {
			it, err := r.NewIterator()
			if err != nil {
				t.Fatalf("NewIterator: %v", err)
			}
			for it.Next() {
				inSST[string(it.Key().Key)] = true
			}
			it.Close()
		}
	}
	t.Logf("distinct user keys found in all SSTables=%d", len(inSST))

	// Now check: for each key, is it in the SST but GetWithTS still returns nil?
	bothMiss := 0
	onlyEngine := 0
	onlySST := 0
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS(%d): %v", i, err)
		}
		gotOK := len(got) == 1 && got[0] == byte(i)
		sstOK := inSST[string(key)]
		switch {
		case gotOK && sstOK:
		case gotOK && !sstOK:
			onlyEngine++
		case !gotOK && sstOK:
			onlySST++
			t.Logf("key %d IS in SST but GetWithTS returned %v", i, got)
		default:
			bothMiss++
		}
	}
	t.Logf("bothMiss=%d onlyEngine=%d onlySST=%d", bothMiss, onlyEngine, onlySST)
	_ = fmt.Sprintf
}
