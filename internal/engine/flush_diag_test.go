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
	"bytes"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/errors"
)

// TestDiagGetBeforeAndAfterFlush isolates whether keys are lost BEFORE flush
// (Put/Get/hash routing problem) or AFTER flush (flush problem).
func TestDiagGetBeforeAndAfterFlush(t *testing.T) {
	dir := t.TempDir()
	eng, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatalf("NewLSMEngine: %v", err)
	}
	defer errors.CloseWithFatal(eng, "engine")

	const n = 128
	beforeMiss := 0
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		if err := eng.PutWithTS(key, []byte{byte(i)}, uint64(i+1)); err != nil {
			t.Fatalf("PutWithTS(%d): %v", i, err)
		}
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS(%d) before flush: %v", i, err)
		}
		if !bytes.Equal(got, []byte{byte(i)}) {
			beforeMiss++
			t.Logf("before flush: key %d got %v want [%d]", i, got, byte(i))
		}
	}
	t.Logf("BEFORE flush misses=%d shards=%d", beforeMiss, len(eng.shards))

	// Flush each shard.
	for _, shard := range eng.shards {
		if err := shard.flushMemTable(); err != nil {
			t.Fatalf("shard.flushMemTable: %v", err)
		}
	}

	// Verify each shard's levels got populated and its memtable drained.
	totalSST := 0
	for idx, shard := range eng.shards {
		shard.levelsMu.RLock()
		ln := len(shard.levels[0])
		shard.levelsMu.RUnlock()
		totalSST += ln
		t.Logf("shard %d: sst=%d memSize=%d", idx, ln, shard.memTable.Size())
	}
	t.Logf("total SSTables after flush=%d", totalSST)

	afterMiss := 0
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		got, err := eng.GetWithTS(key, uint64(i+1))
		if err != nil {
			t.Fatalf("GetWithTS(%d) after flush: %v", i, err)
		}
		if !bytes.Equal(got, []byte{byte(i)}) {
			afterMiss++
			t.Logf("after flush: key %d got %v want [%d]", i, got, byte(i))
		}
	}
	t.Logf("AFTER flush misses=%d", afterMiss)

	if afterMiss > 0 {
		t.Errorf("keys lost after flush: before=%d after=%d", beforeMiss, afterMiss)
	}
}
