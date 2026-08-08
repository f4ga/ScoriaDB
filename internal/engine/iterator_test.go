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
	"testing"

	"github.com/f4ga/ScoriaDB/internal/engine/memtable"
	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// ============================================================
// Test Iterator (helper for mergeIterator tests)
// ============================================================

// testIter is a simple test iterator that yields predefined key-value pairs.
type testIter struct {
	entries []struct{ key, value []byte }
	pos     int
	closed  bool
}

func newTestIter(keys, values [][]byte) *testIter {
	entries := make([]struct{ key, value []byte }, len(keys))
	for i := range keys {
		entries[i] = struct{ key, value []byte }{keys[i], values[i]}
	}
	return &testIter{entries: entries, pos: -1}
}

func (it *testIter) Next() bool {
	if it.closed || it.pos+1 >= len(it.entries) {
		return false
	}
	it.pos++
	return true
}

func (it *testIter) Key() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].key
}

func (it *testIter) Value() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].value
}

func (it *testIter) Err() error      { return nil }
func (it *testIter) Close() error    { it.closed = true; return nil }
func (it *testIter) IsDeleted() bool { return false }

// ============================================================
// mergeIterator Tests
// ============================================================

func TestMergeIteratorSingleSource(t *testing.T) {
	keys := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	vals := [][]byte{[]byte("1"), []byte("2"), []byte("3")}

	mi := &mergeIterator{
		heap: make(iterHeap, 0, 1),
	}
	mi.addSource(newTestIter(keys, vals))

	var gotKeys, gotVals [][]byte
	for mi.Next() {
		gotKeys = append(gotKeys, copyBytes(mi.Key()))
		gotVals = append(gotVals, copyBytes(mi.Value()))
	}

	if len(gotKeys) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(gotKeys))
	}
	if !bytes.Equal(gotKeys[0], []byte("a")) || !bytes.Equal(gotVals[0], []byte("1")) {
		t.Errorf("expected (a,1), got (%s,%s)", gotKeys[0], gotVals[0])
	}
	if !bytes.Equal(gotKeys[1], []byte("b")) || !bytes.Equal(gotVals[1], []byte("2")) {
		t.Errorf("expected (b,2), got (%s,%s)", gotKeys[1], gotVals[1])
	}
	if !bytes.Equal(gotKeys[2], []byte("c")) || !bytes.Equal(gotVals[2], []byte("3")) {
		t.Errorf("expected (c,3), got (%s,%s)", gotKeys[2], gotVals[2])
	}

	mi.Close()
}

func TestMergeIteratorMultipleSources(t *testing.T) {
	s1 := newTestIter(
		[][]byte{[]byte("a"), []byte("c"), []byte("e")},
		[][]byte{[]byte("1"), []byte("3"), []byte("5")},
	)
	s2 := newTestIter(
		[][]byte{[]byte("b"), []byte("d"), []byte("f")},
		[][]byte{[]byte("2"), []byte("4"), []byte("6")},
	)

	mi := &mergeIterator{
		heap: make(iterHeap, 0, 2),
	}
	mi.addSource(s1)
	mi.addSource(s2)

	var gotKeys [][]byte
	for mi.Next() {
		gotKeys = append(gotKeys, copyBytes(mi.Key()))
	}

	if len(gotKeys) != 6 {
		t.Fatalf("expected 6 entries, got %d", len(gotKeys))
	}
	expected := []string{"a", "b", "c", "d", "e", "f"}
	for i, exp := range expected {
		if string(gotKeys[i]) != exp {
			t.Errorf("entry %d: expected key %s, got %s", i, exp, gotKeys[i])
		}
	}

	mi.Close()
}

func TestMergeIteratorDeduplication(t *testing.T) {
	// Source 1 (higher priority): a=1, b=2
	s1 := newTestIter(
		[][]byte{[]byte("a"), []byte("b")},
		[][]byte{[]byte("1"), []byte("2")},
	)
	// Source 2 (lower priority): a=old, c=3
	s2 := newTestIter(
		[][]byte{[]byte("a"), []byte("c")},
		[][]byte{[]byte("old"), []byte("3")},
	)

	mi := &mergeIterator{
		heap: make(iterHeap, 0, 2),
	}
	mi.addSource(s1)
	mi.addSource(s2)

	var gotKeys, gotVals [][]byte
	for mi.Next() {
		gotKeys = append(gotKeys, copyBytes(mi.Key()))
		gotVals = append(gotVals, copyBytes(mi.Value()))
	}

	if len(gotKeys) != 3 {
		t.Fatalf("expected 3 entries (deduplicated), got %d", len(gotKeys))
	}
	// a should come from source 1 (higher priority)
	if string(gotKeys[0]) != "a" || string(gotVals[0]) != "1" {
		t.Errorf("expected (a,1), got (%s,%s)", gotKeys[0], gotVals[0])
	}
	if string(gotKeys[1]) != "b" || string(gotVals[1]) != "2" {
		t.Errorf("expected (b,2), got (%s,%s)", gotKeys[1], gotVals[1])
	}
	if string(gotKeys[2]) != "c" || string(gotVals[2]) != "3" {
		t.Errorf("expected (c,3), got (%s,%s)", gotKeys[2], gotVals[2])
	}

	mi.Close()
}

func TestMergeIteratorEmptySource(t *testing.T) {
	s1 := newTestIter(nil, nil)
	s2 := newTestIter(
		[][]byte{[]byte("a")},
		[][]byte{[]byte("1")},
	)

	mi := &mergeIterator{
		heap: make(iterHeap, 0, 2),
	}
	mi.addSource(s1)
	mi.addSource(s2)

	var gotKeys [][]byte
	for mi.Next() {
		gotKeys = append(gotKeys, copyBytes(mi.Key()))
	}

	if len(gotKeys) != 1 || string(gotKeys[0]) != "a" {
		t.Errorf("expected 1 entry (a), got %v", gotKeys)
	}

	mi.Close()
}

func TestMergeIteratorAllEmpty(t *testing.T) {
	mi := &mergeIterator{
		heap: make(iterHeap, 0, 2),
	}
	mi.addSource(newTestIter(nil, nil))
	mi.addSource(newTestIter(nil, nil))

	if mi.Next() {
		t.Error("expected no entries from empty sources")
	}

	mi.Close()
}

func TestMergeIteratorCloseIdempotent(t *testing.T) {
	s1 := newTestIter(
		[][]byte{[]byte("a")},
		[][]byte{[]byte("1")},
	)

	mi := &mergeIterator{
		heap: make(iterHeap, 0, 1),
	}
	mi.addSource(s1)

	if err := mi.Close(); err != nil {
		t.Errorf("first Close error: %v", err)
	}
	if err := mi.Close(); err != nil {
		t.Errorf("second Close error: %v", err)
	}
}

// ============================================================
// memtableIter Tests
// ============================================================

func TestMemtableIterPrefixFilter(t *testing.T) {
	mt := memtable.NewMemTable()

	mt.Put(mvcc.NewMVCCKey([]byte("aaa:1"), 1), []byte("val1"))
	mt.Put(mvcc.NewMVCCKey([]byte("aaa:2"), 2), []byte("val2"))
	mt.Put(mvcc.NewMVCCKey([]byte("bbb:1"), 3), []byte("val3"))
	mt.Put(mvcc.NewMVCCKey([]byte("bbb:2"), 4), []byte("val4"))

	it := newMemtableIter(mt, []byte("aaa:"))
	var gotKeys, gotVals [][]byte
	for it.Next() {
		gotKeys = append(gotKeys, copyBytes(it.Key()))
		gotVals = append(gotVals, copyBytes(it.Value()))
	}
	it.Close()

	if len(gotKeys) != 2 {
		t.Fatalf("expected 2 entries with prefix 'aaa:', got %d", len(gotKeys))
	}
	if string(gotKeys[0]) != "aaa:1" || string(gotVals[0]) != "val1" {
		t.Errorf("expected (aaa:1, val1), got (%s, %s)", gotKeys[0], gotVals[0])
	}
	if string(gotKeys[1]) != "aaa:2" || string(gotVals[1]) != "val2" {
		t.Errorf("expected (aaa:2, val2), got (%s, %s)", gotKeys[1], gotVals[1])
	}
}

func TestMemtableIterNoMatch(t *testing.T) {
	mt := memtable.NewMemTable()
	mt.Put(mvcc.NewMVCCKey([]byte("aaa:1"), 1), []byte("val1"))

	it := newMemtableIter(mt, []byte("nonexistent:"))
	if it.Next() {
		t.Error("expected no entries for non-matching prefix")
	}
	it.Close()
}

func TestMemtableIterEmpty(t *testing.T) {
	mt := memtable.NewMemTable()
	it := newMemtableIter(mt, []byte("prefix:"))
	if it.Next() {
		t.Error("expected no entries from empty MemTable")
	}
	it.Close()
}

func TestMemtableIterSkipsTombstones(t *testing.T) {
	mt := memtable.NewMemTable()

	mt.Put(mvcc.NewMVCCKey([]byte("key:1"), 1), []byte("val1"))
	mt.DeleteWithTS(mvcc.NewMVCCKey([]byte("key:1"), 2))

	// The SkipListIterator already skips deleted nodes internally.
	// memtableIter only does prefix filtering — the tombstone is already
	// filtered by the skip list iterator.
	it := newMemtableIter(mt, []byte("key:"))
	count := 0
	for it.Next() {
		count++
	}
	it.Close()
	// SkipListIterator skips deleted nodes, so only the non-deleted version is visible
	if count != 1 {
		t.Errorf("expected 1 entry (skip list skips deleted), got %d", count)
	}
}

func TestMVCCIteratorSkipsTombstones(t *testing.T) {
	mt := memtable.NewMemTable()

	mt.Put(mvcc.NewMVCCKey([]byte("key:1"), 1), []byte("val1"))
	mt.DeleteWithTS(mvcc.NewMVCCKey([]byte("key:1"), 2))

	// MVCCIterator wraps memtableIter. The skip list already skips deleted nodes,
	// so the tombstone is not visible. MVCCIterator should still return the
	// non-deleted version.
	raw := newMemtableIter(mt, []byte("key:"))
	it := NewMVCCIterator(raw)
	if !it.Next() {
		t.Fatal("expected the non-deleted version to be visible")
	}
	if string(it.Key()) != "key:1" || string(it.Value()) != "val1" {
		t.Errorf("expected (key:1, val1), got (%s, %s)", it.Key(), it.Value())
	}
	if it.Next() {
		t.Error("expected only one entry")
	}
	it.Close()
}

// ============================================================
// Scan Integration Tests (new tests not in engine_test.go)
// ============================================================

func TestScanWithPrefix(t *testing.T) {
	logger.SetLevel(logger.ERROR)
	dir := t.TempDir()
	e, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	e.PutWithTS([]byte("aaa:1"), []byte("val1"), 1)
	e.PutWithTS([]byte("aaa:2"), []byte("val2"), 2)
	e.PutWithTS([]byte("bbb:1"), []byte("val3"), 3)
	e.PutWithTS([]byte("bbb:2"), []byte("val4"), 4)

	iter := e.Scan([]byte("aaa:"))
	var gotKeys, gotVals [][]byte
	for iter.Next() {
		gotKeys = append(gotKeys, copyBytes(iter.Key()))
		gotVals = append(gotVals, copyBytes(iter.Value()))
	}
	if err := iter.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	iter.Close()

	if len(gotKeys) != 2 {
		t.Fatalf("expected 2 entries with prefix 'aaa:', got %d", len(gotKeys))
	}
	if string(gotKeys[0]) != "aaa:1" || string(gotVals[0]) != "val1" {
		t.Errorf("expected (aaa:1, val1), got (%s, %s)", gotKeys[0], gotVals[0])
	}
	if string(gotKeys[1]) != "aaa:2" || string(gotVals[1]) != "val2" {
		t.Errorf("expected (aaa:2, val2), got (%s, %s)", gotKeys[1], gotVals[1])
	}
}

func TestScanDeduplicationAcrossSources(t *testing.T) {
	logger.SetLevel(logger.ERROR)
	dir := t.TempDir()
	e, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	e.PutWithTS([]byte("key:1"), []byte("old"), 1)
	e.PutWithTS([]byte("key:1"), []byte("new"), 2)

	iter := e.Scan([]byte("key:"))
	var gotKeys, gotVals [][]byte
	for iter.Next() {
		gotKeys = append(gotKeys, copyBytes(iter.Key()))
		gotVals = append(gotVals, copyBytes(iter.Value()))
	}
	if err := iter.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	iter.Close()

	if len(gotKeys) != 1 {
		t.Fatalf("expected 1 entry (deduplicated), got %d", len(gotKeys))
	}
	if string(gotVals[0]) != "new" {
		t.Errorf("expected value 'new', got '%s'", gotVals[0])
	}
}

func TestScanMultipleCalls(t *testing.T) {
	logger.SetLevel(logger.ERROR)
	dir := t.TempDir()
	e, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	for i := 0; i < 10; i++ {
		key := []byte{byte('a' + i)}
		e.PutWithTS(key, []byte("val"), uint64(i+1))
	}

	iter1 := e.Scan([]byte("a"))
	count1 := 0
	for iter1.Next() {
		count1++
	}
	iter1.Close()

	iter2 := e.Scan([]byte("a"))
	count2 := 0
	for iter2.Next() {
		count2++
	}
	iter2.Close()

	if count1 != count2 {
		t.Errorf("expected same count from two scans: %d vs %d", count1, count2)
	}
}

func TestScanLargeDataset(t *testing.T) {
	logger.SetLevel(logger.ERROR)
	dir := t.TempDir()
	e, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	for i := 0; i < 1000; i++ {
		key := []byte{'s', 'c', 'a', 'n', ':', byte(i >> 8), byte(i & 0xff)}
		e.PutWithTS(key, []byte("value"), uint64(i+1))
	}

	iter := e.Scan([]byte("scan:"))
	count := 0
	for iter.Next() {
		count++
	}
	if err := iter.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	iter.Close()

	if count != 1000 {
		t.Errorf("expected 1000 entries, got %d", count)
	}
}

func TestScanLimitResults(t *testing.T) {
	logger.SetLevel(logger.ERROR)
	dir := t.TempDir()
	e, err := NewLSMEngine(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	for i := 0; i < 1000; i++ {
		key := []byte{'s', 'c', 'a', 'n', ':', byte(i >> 8), byte(i & 0xff)}
		e.PutWithTS(key, []byte("value"), uint64(i+1))
	}

	iter := e.Scan([]byte("scan:"))
	count := 0
	for iter.Next() {
		count++
		if count >= 100 {
			break
		}
	}
	if err := iter.Err(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	iter.Close()

	if count != 100 {
		t.Errorf("expected 100 entries (limited), got %d", count)
	}
}

// ============================================================
// Helpers
// ============================================================

// copyBytes returns a copy of the byte slice.
func copyBytes(src []byte) []byte {
	if src == nil {
		return nil
	}
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

// ============================================================
// Tombstone-aware test iterator for MVCCIterator tests
// ============================================================

// tombstoneIter is a test iterator that yields entries in (userKey, ascending
// commitTS) order and reports IsDeleted() for tombstones (nil value).
type tombstoneIter struct {
	entries []struct{ key, value []byte }
	pos     int
	closed  bool
}

func newTombstoneIter(keys, values [][]byte) *tombstoneIter {
	entries := make([]struct{ key, value []byte }, len(keys))
	for i := range keys {
		entries[i] = struct{ key, value []byte }{keys[i], values[i]}
	}
	return &tombstoneIter{entries: entries, pos: -1}
}

func (it *tombstoneIter) Next() bool {
	if it.closed || it.pos+1 >= len(it.entries) {
		return false
	}
	it.pos++
	return true
}

func (it *tombstoneIter) Key() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].key
}

func (it *tombstoneIter) Value() []byte {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return nil
	}
	return it.entries[it.pos].value
}

func (it *tombstoneIter) Err() error   { return nil }
func (it *tombstoneIter) Close() error { it.closed = true; return nil }
func (it *tombstoneIter) IsDeleted() bool {
	if it.pos < 0 || it.pos >= len(it.entries) {
		return false
	}
	return it.entries[it.pos].value == nil
}

// TestMVCCIteratorTombstoneOrder verifies MVCCIterator correctly handles
// tombstones that appear before or between live versions of the same user key:
//   - [tombstone, live]     → the live version is returned (re-insert after delete).
//   - [live, tombstone]     → the key is skipped (deleted last).
//   - [live, tombstone, live] → the last live version is returned.
//   - Multiple keys interleaved.
func TestMVCCIteratorTombstoneOrder(t *testing.T) {
	cases := []struct {
		name     string
		keys     [][]byte
		values   [][]byte
		wantKeys []string
		wantVals []string
	}{
		{
			name: "tombstone then live",
			keys: [][]byte{[]byte("k"), []byte("k")},
			// nil value = tombstone (oldest), then a live value (newest).
			values:   [][]byte{nil, []byte("live")},
			wantKeys: []string{"k"},
			wantVals: []string{"live"},
		},
		{
			name: "live then tombstone",
			keys: [][]byte{[]byte("k"), []byte("k")},
			// live (oldest), then tombstone (newest) — key is deleted.
			values:   [][]byte{[]byte("v1"), nil},
			wantKeys: nil,
			wantVals: nil,
		},
		{
			name: "live tombstone live",
			keys: [][]byte{[]byte("k"), []byte("k"), []byte("k")},
			// live, then tombstone, then live again — newest visible is live.
			values:   [][]byte{[]byte("v1"), nil, []byte("v3")},
			wantKeys: []string{"k"},
			wantVals: []string{"v3"},
		},
		{
			name: "multiple keys",
			keys: [][]byte{
				[]byte("a"), []byte("a"), // tombstone then live → "a"
				[]byte("b"), []byte("b"), // live then tombstone → skip
				[]byte("c"), []byte("c"), []byte("c"), // live, tombstone, live → "c2"
			},
			values: [][]byte{
				nil, []byte("a-live"),
				[]byte("b-v1"), nil,
				[]byte("c-v1"), nil, []byte("c-v2"),
			},
			wantKeys: []string{"a", "c"},
			wantVals: []string{"a-live", "c-v2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inner := newTombstoneIter(tc.keys, tc.values)
			it := NewMVCCIterator(inner)
			defer it.Close()

			var gotKeys, gotVals []string
			for it.Next() {
				gotKeys = append(gotKeys, string(it.Key()))
				gotVals = append(gotVals, string(it.Value()))
			}
			if err := it.Err(); err != nil {
				t.Fatalf("unexpected iterator error: %v", err)
			}

			if len(gotKeys) != len(tc.wantKeys) {
				t.Fatalf("got %d keys %v, want %d keys %v",
					len(gotKeys), gotKeys, len(tc.wantKeys), tc.wantKeys)
			}
			for i := range gotKeys {
				if gotKeys[i] != tc.wantKeys[i] {
					t.Errorf("key[%d]=%q, want %q", i, gotKeys[i], tc.wantKeys[i])
				}
				if gotVals[i] != tc.wantVals[i] {
					t.Errorf("val[%d]=%q, want %q", i, gotVals[i], tc.wantVals[i])
				}
			}
		})
	}
}

// Ensure compile-time interface satisfaction
var _ Iterator = (*testIter)(nil)
var _ Iterator = (*tombstoneIter)(nil)
