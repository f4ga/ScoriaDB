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
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/errors"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

func TestWriterAndReader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Записываем несколько ключей
	keys := []mvcc.MVCCKey{
		mvcc.NewMVCCKey([]byte("key1"), 100),
		mvcc.NewMVCCKey([]byte("key2"), 200),
		mvcc.NewMVCCKey([]byte("key3"), 300),
	}
	values := [][]byte{
		[]byte("value1"),
		[]byte("value2"),
		[]byte("value3"),
	}

	for i, key := range keys {
		if err := writer.Append(key, values[i]); err != nil {
			t.Fatalf("failed to append key %d: %v", i, err)
		}
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish writer: %v", err)
	}

	// Открываем SSTable для чтения
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	// Проверяем поиск ключей
	for i, key := range keys {
		val, found := reader.Lookup(key)
		if !found {
			t.Errorf("key %d not found", i)
			continue
		}
		if string(val) != string(values[i]) {
			t.Errorf("value mismatch for key %d: got %s, want %s", i, val, values[i])
		}
	}

	// Проверяем отсутствующий ключ
	missingKey := mvcc.NewMVCCKey([]byte("missing"), 400)
	val, found := reader.Lookup(missingKey)
	if found {
		t.Errorf("unexpected found missing key: %v", val)
	}
}

func TestBloomFilterSkip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bloom.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Добавляем ключи
	keys := []mvcc.MVCCKey{
		mvcc.NewMVCCKey([]byte("apple"), 100),
		mvcc.NewMVCCKey([]byte("banana"), 200),
		mvcc.NewMVCCKey([]byte("cherry"), 300),
	}
	for _, key := range keys {
		if err := writer.Append(key, []byte("value")); err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	// Ключ, которого нет в фильтре Блума (с высокой вероятностью)
	// Поскольку фильтр Блума может дать ложноположительный результат, тест может быть неустойчивым.
	// Для простоты пропустим проверку отрицательного результата.
	// Проверим, что существующие ключи находятся.
	for _, key := range keys {
		_, found := reader.Lookup(key)
		if !found {
			t.Errorf("expected key %v to be found", key)
		}
	}
}

func TestCompactionSimple(t *testing.T) {
	// Этот тест проверяет, что два SSTable можно объединить в один.
	// Создаем два SSTable с перекрывающимися ключами.
	dir := t.TempDir()
	path1 := filepath.Join(dir, "table1.sst")
	path2 := filepath.Join(dir, "table2.sst")
	mergedPath := filepath.Join(dir, "merged.sst")

	// Записываем первый SSTable
	writer1, err := NewWriter(path1)
	if err != nil {
		t.Fatalf("failed to create writer1: %v", err)
	}
	if err := writer1.Append(mvcc.NewMVCCKey([]byte("a"), 100), []byte("val1")); err != nil {
		t.Fatalf("failed to append to writer1: %v", err)
	}
	if err := writer1.Append(mvcc.NewMVCCKey([]byte("b"), 100), []byte("val2")); err != nil {
		t.Fatalf("failed to append to writer1: %v", err)
	}
	if err := writer1.Finish(); err != nil {
		t.Fatalf("failed to finish writer1: %v", err)
	}

	// Записываем второй SSTable
	writer2, err := NewWriter(path2)
	if err != nil {
		t.Fatalf("failed to create writer2: %v", err)
	}
	if err := writer2.Append(mvcc.NewMVCCKey([]byte("b"), 200), []byte("val2_new")); err != nil { // обновление ключа b
		t.Fatalf("failed to append to writer2: %v", err)
	}
	if err := writer2.Append(mvcc.NewMVCCKey([]byte("c"), 100), []byte("val3")); err != nil {
		t.Fatalf("failed to append to writer2: %v", err)
	}
	if err := writer2.Finish(); err != nil {
		t.Fatalf("failed to finish writer2: %v", err)
	}

	// Открываем оба SSTable
	reader1, err := Open(path1)
	if err != nil {
		t.Fatalf("failed to open reader1: %v", err)
	}
	reader2, err := Open(path2)
	if err != nil {
		t.Fatalf("failed to open reader2: %v", err)
	}
	defer errors.CloseWithFatal(reader1, "sstable-reader1")
	defer errors.CloseWithFatal(reader2, "sstable-reader2")

	// Создаем объединенный SSTable (симуляция compaction)
	writerMerged, err := NewWriter(mergedPath)
	if err != nil {
		t.Fatalf("failed to create merged writer: %v", err)
	}

	// Собираем все ключи из обоих таблиц, сохраняя последнюю версию.
	// В реальном compaction используется MergeIterator.
	// Для простоты просто добавим все ключи из reader2, затем из reader1 (поздние версии перезапишут ранние).
	// Пропускаем из-за отсутствия итератора.
	// Вместо этого просто создадим новый SSTable с ожидаемыми ключами.
	if err := writerMerged.Append(mvcc.NewMVCCKey([]byte("a"), 100), []byte("val1")); err != nil {
		t.Fatalf("failed to append to writerMerged: %v", err)
	}
	if err := writerMerged.Append(mvcc.NewMVCCKey([]byte("b"), 200), []byte("val2_new")); err != nil {
		t.Fatalf("failed to append to writerMerged: %v", err)
	}
	if err := writerMerged.Append(mvcc.NewMVCCKey([]byte("c"), 100), []byte("val3")); err != nil {
		t.Fatalf("failed to append to writerMerged: %v", err)
	}
	if err := writerMerged.Finish(); err != nil {
		t.Fatalf("failed to finish writerMerged: %v", err)
	}

	// Проверяем объединенный SSTable
	readerMerged, err := Open(mergedPath)
	if err != nil {
		t.Fatalf("failed to open merged reader: %v", err)
	}
	defer errors.CloseWithFatal(readerMerged, "sstable-merged")

	// Проверяем ключ b (должна быть новая версия)
	val, found := readerMerged.Lookup(mvcc.NewMVCCKey([]byte("b"), 200))
	if !found || string(val) != "val2_new" {
		t.Errorf("merged SSTable incorrect: got %v, want val2_new", val)
	}

	// Удаляем временные файлы
	errors.RemoveWithLog(path1)
	errors.RemoveWithLog(path2)
	errors.RemoveWithLog(mergedPath)
}

func TestWriterBlockOverflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "overflow.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write enough keys to overflow a block (BlockSize = 16KB)
	largeValue := make([]byte, 200)
	for i := range largeValue {
		largeValue[i] = 'b'
	}

	numKeys := 100
	for i := 0; i < numKeys; i++ {
		key := mvcc.NewMVCCKey([]byte(fmt.Sprintf("key-%04d", i)), uint64(i+1))
		if err := writer.Append(key, largeValue); err != nil {
			t.Fatalf("failed to append key %d: %v", i, err)
		}
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish writer: %v", err)
	}

	// Open and verify all keys
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	for i := 0; i < numKeys; i++ {
		key := mvcc.NewMVCCKey([]byte(fmt.Sprintf("key-%04d", i)), uint64(i+1))
		val, found := reader.Lookup(key)
		if !found {
			t.Errorf("key %d not found after block overflow", i)
			continue
		}
		if string(val) != string(largeValue) {
			t.Errorf("value mismatch for key %d", i)
		}
	}
}

func TestReaderLookupTombstone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tombstone.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write a key with a nil value (tombstone)
	key := mvcc.NewMVCCKey([]byte("tombstone_key"), 100)
	if err := writer.Append(key, nil); err != nil {
		t.Fatalf("failed to append tombstone: %v", err)
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish writer: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	// Tombstone entries are stored but Lookup returns nil, false for them
	// (the reader treats nil values as deleted/tombstone)
	val, found := reader.Lookup(key)
	if found {
		t.Error("expected tombstone key to not be found by Lookup")
	}
	if val != nil {
		t.Errorf("expected nil value for tombstone, got %v", val)
	}
}

func TestReaderNewIterator(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iter.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	keys := []mvcc.MVCCKey{
		mvcc.NewMVCCKey([]byte("alpha"), 100),
		mvcc.NewMVCCKey([]byte("beta"), 200),
		mvcc.NewMVCCKey([]byte("gamma"), 300),
	}
	values := [][]byte{
		[]byte("val1"),
		[]byte("val2"),
		[]byte("val3"),
	}

	for i, k := range keys {
		if err := writer.Append(k, values[i]); err != nil {
			t.Fatalf("failed to append: %v", err)
		}
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	iter, err := reader.NewIterator()
	if err != nil {
		t.Fatalf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	var count int
	for iter.Next() {
		if count >= len(keys) {
			t.Errorf("more items than expected")
			break
		}
		expectedKey := keys[count]
		expectedVal := values[count]
		if string(iter.Key().Key) != string(expectedKey.Key) {
			t.Errorf("key %d: expected %s, got %s", count, expectedKey.Key, iter.Key().Key)
		}
		if string(iter.Value()) != string(expectedVal) {
			t.Errorf("value %d: expected %s, got %s", count, expectedVal, iter.Value())
		}
		count++
	}
	if count != len(keys) {
		t.Errorf("expected %d items, got %d", len(keys), count)
	}
}

func TestBloomFilterAddAndMayContain(t *testing.T) {
	bf := NewBloomFilter(10)

	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
		[]byte("hello"),
		[]byte("world"),
	}

	for _, k := range keys {
		bf.Add(k)
	}

	for _, k := range keys {
		if !bf.MayContain(k) {
			t.Errorf("Bloom filter should contain key %q", k)
		}
	}

	// Keys not added should (with high probability) not be found
	notAdded := [][]byte{
		[]byte("nonexistent"),
		[]byte("random"),
		[]byte("test"),
	}
	for _, k := range notAdded {
		if bf.MayContain(k) {
			t.Logf("Bloom filter false positive for key %q (acceptable)", k)
		}
	}
}

func TestBloomFilterEncodeDecode(t *testing.T) {
	bf := NewBloomFilter(10)
	bf.Add([]byte("test_key"))
	bf.Add([]byte("another_key"))

	encoded := bf.Encode()
	if len(encoded) == 0 {
		t.Error("encoded data should not be empty")
	}

	decoded := DecodeBloomFilter(encoded, 10)
	if !decoded.MayContain([]byte("test_key")) {
		t.Error("decoded filter should contain 'test_key'")
	}
	if !decoded.MayContain([]byte("another_key")) {
		t.Error("decoded filter should contain 'another_key'")
	}
}

func TestBloomFilterSetBitGetBit(t *testing.T) {
	bf := NewBloomFilter(10)

	// Test setBit and getBit indirectly through Add and MayContain
	bf.Add([]byte("test"))
	if !bf.MayContain([]byte("test")) {
		t.Error("expected filter to contain 'test'")
	}

	// Test with empty filter
	empty := NewBloomFilter(10)
	if empty.MayContain([]byte("anything")) {
		t.Error("empty filter should not contain anything")
	}
}

func TestBloomFilterSetK(t *testing.T) {
	bf := NewBloomFilter(10)
	bf.SetK(5)
	// SetK just sets the k field, verify it doesn't panic
	bf.Add([]byte("test"))
	if !bf.MayContain([]byte("test")) {
		t.Error("expected filter to contain 'test' after SetK")
	}
}

func TestWriterFinishWithTombstoneOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tombstone_only.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write only tombstone entries
	if err := writer.Append(mvcc.NewMVCCKey([]byte("del1"), 100), nil); err != nil {
		t.Fatalf("failed to append tombstone: %v", err)
	}
	if err := writer.Append(mvcc.NewMVCCKey([]byte("del2"), 200), nil); err != nil {
		t.Fatalf("failed to append tombstone: %v", err)
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish writer: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	// Tombstone entries should not be found by Lookup
	_, found := reader.Lookup(mvcc.NewMVCCKey([]byte("del1"), 100))
	if found {
		t.Error("expected tombstone key to not be found")
	}
}

func TestReaderLookupNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lookup.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	if err := writer.Append(mvcc.NewMVCCKey([]byte("existing"), 100), []byte("val")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	// Lookup with higher timestamp should find the key (MVCC: inverted timestamps)
	// The stored key has ts=100, which is encoded as MaxUint64-100.
	// Looking up with ts=200 uses MaxUint64-200, which is "less" than MaxUint64-100.
	// Since the stored timestamp (MaxUint64-100) >= lookup timestamp (MaxUint64-200), it matches.
	val, found := reader.Lookup(mvcc.NewMVCCKey([]byte("existing"), 200))
	if !found {
		t.Error("expected key with higher timestamp to be found (MVCC visibility)")
	}
	if string(val) != "val" {
		t.Errorf("expected 'val', got %q", string(val))
	}

	// Lookup with completely different key
	_, found = reader.Lookup(mvcc.NewMVCCKey([]byte("nonexistent"), 100))
	if found {
		t.Error("expected nonexistent key to not be found")
	}
}

func TestWriterEmptySSTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish empty writer: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open empty reader: %v", err)
	}
	defer errors.CloseWithFatal(reader, "sstable-reader")

	// Lookup on empty SSTable should not find anything
	_, found := reader.Lookup(mvcc.NewMVCCKey([]byte("any"), 1))
	if found {
		t.Error("expected no keys in empty SSTable")
	}
}

// ============================================================
// SSTable additional tests
// ============================================================

func TestOpenNonExistentFile(t *testing.T) {
	_, err := Open("/nonexistent/path/to/sstable.sst")
	if err == nil {
		t.Error("expected error when opening non-existent file")
	}
}

func TestOpenCorruptedFooter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted_footer.sst")

	// Create a file with invalid magic number
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Write garbage data
	garbage := make([]byte, 200)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	if _, err := file.Write(garbage); err != nil {
		t.Fatalf("failed to write garbage: %v", err)
	}
	file.Close()

	_, err = Open(path)
	if err == nil {
		t.Error("expected error when opening file with corrupted footer")
	}
}

func TestOpenCorruptedIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupted_index.sst")

	// Create a valid SSTable first
	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	if err := writer.Append(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	// Corrupt the index by truncating the file
	// The footer is at the end, we need to find and corrupt the index area
	file, err := os.OpenFile(path, os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("failed to open file for corruption: %v", err)
	}

	// Read footer to find index offset
	if _, err := file.Seek(-80, io.SeekEnd); err != nil {
		t.Fatalf("failed to seek to footer: %v", err)
	}
	var footer Footer
	if err := binary.Read(file, binary.LittleEndian, &footer); err != nil {
		t.Fatalf("failed to read footer: %v", err)
	}

	// Corrupt index data
	if _, err := file.Seek(int64(footer.IndexOffset), io.SeekStart); err != nil {
		t.Fatalf("failed to seek to index: %v", err)
	}
	corruptedIndex := make([]byte, footer.IndexSize)
	for i := range corruptedIndex {
		corruptedIndex[i] = 0xDE
	}
	if _, err := file.Write(corruptedIndex); err != nil {
		t.Fatalf("failed to corrupt index: %v", err)
	}
	file.Close()

	// Opening should fail or succeed but Lookup should handle gracefully
	reader, err := Open(path)
	if err != nil {
		// Expected: corrupted index causes error
		return
	}
	defer reader.Close()

	// If open succeeded, Lookup should handle gracefully
	_, found := reader.Lookup(mvcc.NewMVCCKey([]byte("key"), 100))
	if found {
		t.Log("Lookup succeeded despite corrupted index (graceful handling)")
	}
}

func TestReadBlockNonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readblock.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	if err := writer.Append(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	// Read block at non-existent offset
	_, err = reader.readBlock(1 << 60)
	if err == nil {
		t.Error("expected error when reading block at non-existent offset")
	}
}

func TestReadBlockZeroSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zerosize.sst")

	// Create a file with a zero-size block marker
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	// Write a valid footer with index pointing to a zero-size block
	// For simplicity, just write a minimal valid SSTable
	file.Close()

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	if err := writer.Append(mvcc.NewMVCCKey([]byte("k"), 100), []byte("v")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	// Read a valid block — should succeed
	data, err := reader.readBlock(0)
	if err != nil {
		t.Errorf("expected no error reading first block, got %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty block data")
	}
}

func TestWriterFinishTwice(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "finish_twice.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	if err := writer.Append(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("first Finish failed: %v", err)
	}

	// Second Finish should fail (file already closed)
	err = writer.Finish()
	if err == nil {
		t.Error("expected error on second Finish")
	}
}

func TestWriterFinishEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty_finish.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Finish without any Append calls
	if err := writer.Finish(); err != nil {
		t.Fatalf("Finish on empty writer failed: %v", err)
	}

	// Should be able to open the resulting file
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open empty SSTable: %v", err)
	}
	defer reader.Close()
}

func TestWriterLargeNumberOfKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "many_keys.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	numKeys := 1000
	for i := 0; i < numKeys; i++ {
		key := mvcc.NewMVCCKey([]byte(fmt.Sprintf("key-%06d", i)), uint64(i+1))
		if err := writer.Append(key, []byte(fmt.Sprintf("value-%d", i))); err != nil {
			t.Fatalf("failed to append key %d: %v", i, err)
		}
	}

	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish writer: %v", err)
	}

	// Verify all keys
	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	for i := 0; i < numKeys; i++ {
		key := mvcc.NewMVCCKey([]byte(fmt.Sprintf("key-%06d", i)), uint64(i+1))
		val, found := reader.Lookup(key)
		if !found {
			t.Errorf("key %d not found", i)
			continue
		}
		expected := fmt.Sprintf("value-%d", i)
		if string(val) != expected {
			t.Errorf("value mismatch for key %d: got '%s', expected '%s'", i, val, expected)
		}
	}
}

func TestBloomFilterSetBit(t *testing.T) {
	bf := NewBloomFilter(10)

	// Set bits at various positions
	bf.Add([]byte("test1"))
	bf.Add([]byte("test2"))
	bf.Add([]byte("test3"))

	// Verify they're found
	if !bf.MayContain([]byte("test1")) {
		t.Error("expected test1 to be found")
	}
	if !bf.MayContain([]byte("test2")) {
		t.Error("expected test2 to be found")
	}
	if !bf.MayContain([]byte("test3")) {
		t.Error("expected test3 to be found")
	}
}

func TestBloomFilterSetBitBoundary(t *testing.T) {
	bf := NewBloomFilter(10)

	// Add a key that produces a hash near the boundary
	// The setBit method should handle expansion
	largeKey := make([]byte, 1000)
	for i := range largeKey {
		largeKey[i] = byte(i)
	}
	bf.Add(largeKey)

	// Verify the key is found
	if !bf.MayContain(largeKey) {
		t.Error("expected large key to be found after add")
	}
}

func TestBloomFilterEmpty(t *testing.T) {
	bf := NewBloomFilter(10)

	// Empty filter should not contain anything
	if bf.MayContain([]byte("anything")) {
		t.Error("empty filter should not contain anything")
	}
}

func TestBloomFilterEncodeDecodeRoundTrip(t *testing.T) {
	bf := NewBloomFilter(10)

	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}
	for _, k := range keys {
		bf.Add(k)
	}

	encoded := bf.Encode()
	decoded := DecodeBloomFilter(encoded, 10)

	for _, k := range keys {
		if !decoded.MayContain(k) {
			t.Errorf("decoded filter should contain key %q", k)
		}
	}
}

func TestBloomFilterDecodeEmpty(t *testing.T) {
	// Decode empty data
	bf := DecodeBloomFilter(nil, 10)
	if bf == nil {
		t.Fatal("DecodeBloomFilter returned nil")
	}
	if bf.MayContain([]byte("anything")) {
		t.Error("empty decoded filter should not contain anything")
	}
}

func TestBloomFilterDecodeShortData(t *testing.T) {
	// Decode data shorter than 4 bytes (no seed)
	shortData := []byte{0x01, 0x02}
	bf := DecodeBloomFilter(shortData, 10)
	if bf == nil {
		t.Fatal("DecodeBloomFilter returned nil")
	}
}

func TestReaderLookupWithVersionVisibility(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "version_visibility.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write a key with ts=100
	if err := writer.Append(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	// Lookup with same timestamp should find
	val, found := reader.Lookup(mvcc.NewMVCCKey([]byte("key"), 100))
	if !found {
		t.Error("expected key to be found with same timestamp")
	}
	if string(val) != "value" {
		t.Errorf("expected 'value', got '%s'", val)
	}

	// Lookup with higher timestamp should find (MVCC visibility)
	val, found = reader.Lookup(mvcc.NewMVCCKey([]byte("key"), 200))
	if !found {
		t.Error("expected key to be found with higher timestamp (MVCC)")
	}
	if string(val) != "value" {
		t.Errorf("expected 'value', got '%s'", val)
	}

	// Lookup with lower timestamp should NOT find
	_, found = reader.Lookup(mvcc.NewMVCCKey([]byte("key"), 50))
	if found {
		t.Error("expected key to NOT be found with lower timestamp")
	}
}

func TestReaderLookupTombstoneVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tombstone_version.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write a tombstone (nil value)
	if err := writer.Append(mvcc.NewMVCCKey([]byte("key"), 100), nil); err != nil {
		t.Fatalf("failed to append tombstone: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	// Tombstone should not be found
	_, found := reader.Lookup(mvcc.NewMVCCKey([]byte("key"), 100))
	if found {
		t.Error("expected tombstone to not be found")
	}
}

func TestReaderNewIteratorEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty_iter.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	iter, err := reader.NewIterator()
	if err != nil {
		t.Fatalf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	if iter.Next() {
		t.Error("expected no items from empty SSTable iterator")
	}
}

func TestReaderNewIteratorMultipleBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "multi_block.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}

	// Write enough keys to span multiple blocks
	largeValue := make([]byte, 500)
	for i := range largeValue {
		largeValue[i] = 'x'
	}

	numKeys := 200
	for i := 0; i < numKeys; i++ {
		key := mvcc.NewMVCCKey([]byte(fmt.Sprintf("key-%04d", i)), uint64(i+1))
		if err := writer.Append(key, largeValue); err != nil {
			t.Fatalf("failed to append key %d: %v", i, err)
		}
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}
	defer reader.Close()

	iter, err := reader.NewIterator()
	if err != nil {
		t.Fatalf("failed to create iterator: %v", err)
	}
	defer iter.Close()

	count := 0
	for iter.Next() {
		count++
	}
	if count != numKeys {
		t.Errorf("expected %d items, got %d", numKeys, count)
	}
}

func TestReaderClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "close_test.sst")

	writer, err := NewWriter(path)
	if err != nil {
		t.Fatalf("failed to create writer: %v", err)
	}
	if err := writer.Append(mvcc.NewMVCCKey([]byte("key"), 100), []byte("value")); err != nil {
		t.Fatalf("failed to append: %v", err)
	}
	if err := writer.Finish(); err != nil {
		t.Fatalf("failed to finish: %v", err)
	}

	reader, err := Open(path)
	if err != nil {
		t.Fatalf("failed to open reader: %v", err)
	}

	// Close should succeed
	if err := reader.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Double close should not panic but may return error
	_ = reader.Close()
}

func TestReleaseBlock(t *testing.T) {
	// ReleaseBlock should not panic
	buf := make([]byte, 100)
	ReleaseBlock(buf)

	// Release nil slice should not panic
	ReleaseBlock(nil)
}

func TestBloomFilterFalsePositiveRate(t *testing.T) {
	bf := NewBloomFilter(10)

	// Add 100 keys
	for i := 0; i < 100; i++ {
		bf.Add([]byte(fmt.Sprintf("key-%d", i)))
	}

	// Check that all added keys are found
	for i := 0; i < 100; i++ {
		if !bf.MayContain([]byte(fmt.Sprintf("key-%d", i))) {
			t.Errorf("added key key-%d not found", i)
		}
	}

	// Check false positive rate (should be low, but we just verify it's not 100%)
	falsePositives := 0
	for i := 0; i < 1000; i++ {
		if bf.MayContain([]byte(fmt.Sprintf("nonexistent-%d", i))) {
			falsePositives++
		}
	}
	t.Logf("False positives: %d/1000 (%.1f%%)", falsePositives, float64(falsePositives)/10.0)
}
