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

package sstable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// TestDiagLookupAllKeys writes N keys to an SSTable, reopens it, and verifies
// that Lookup finds every key. This isolates whether key loss occurs in the
// writer/reader (Bloom filter, block index) or in the flush integration.
// See: SST-03.
func TestDiagLookupAllKeys(t *testing.T) {
	const n = 128
	path := filepath.Join(t.TempDir(), "diag.sst")

	w, err := NewWriter(path, n)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	keys := make([]mvcc.MVCCKey, n)
	for i := 0; i < n; i++ {
		key := []byte{byte(i), byte(i >> 8), 0x11, 0x22}
		keys[i] = mvcc.NewMVCCKey(key, uint64(i+1))
		// Values must be in tagged storage format (leading type tag). The reader
		// strips the tag; raw untagged values whose first byte collides with a
		// tag constant (e.g. 0x02 == tagTombstone) would be misread as tombstones.
		if err := w.AppendTagged(keys[i], []byte{tagInline, byte(i)}); err != nil {
			t.Fatalf("Append(%d): %v", i, err)
		}
	}
	if err := w.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() {
		_ = r.Close()
		_ = os.Remove(path)
	}()

	if r.NumKeys() != uint64(n) {
		t.Errorf("footer.NumKeys=%d, want %d", r.NumKeys(), n)
	}

	// Direct Bloom filter check: every added key must pass MayContain.
	for i := 0; i < n; i++ {
		if !r.bloomFilter.MayContain(keys[i].Key) {
			t.Errorf("Bloom filter rejects present key %d", i)
		}
	}

	// Lookup each key.
	found := 0
	var missing []int
	for i := 0; i < n; i++ {
		if v, ok := r.Lookup(keys[i]); ok {
			found++
			if len(v) == 1 && v[0] != byte(i) {
				t.Errorf("Lookup(%d) returned wrong value %v", i, v)
			}
		} else {
			missing = append(missing, i)
		}
	}
	t.Logf("Lookup found %d/%d keys; missing=%v", found, n, missing)
	if found != n {
		t.Errorf("Lookup found %d/%d keys", found, n)
	}
}
