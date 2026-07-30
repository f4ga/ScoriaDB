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
	"testing"

	"github.com/f4ga/ScoriaDB/internal/logger"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// BenchmarkSSTableRead measures Lookup in an SSTable with 100K entries.
func BenchmarkSSTableRead(b *testing.B) {
	logger.SetLevel(logger.ERROR)

	dir, err := os.MkdirTemp("", "sstable-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	path := dir + "/test.sst"
	w, err := NewWriter(path, 100000)
	if err != nil {
		b.Fatal(err)
	}

	for i := 0; i < 100000; i++ {
		key := mvcc.NewMVCCKey([]byte{byte(i >> 8), byte(i)}, uint64(i+1))
		value := []byte{byte(i % 256)}
		if err := w.Append(key, value); err != nil {
			b.Fatal(err)
		}
	}
	if err := w.Finish(); err != nil {
		b.Fatal(err)
	}

	reader, err := Open(path)
	if err != nil {
		b.Fatal(err)
	}
	defer reader.Close()

	b.ResetTimer()
	const numKeys = 100000
	for i := 0; i < b.N; i++ {
		idx := i % numKeys
		key := mvcc.NewMVCCKey([]byte{byte(idx >> 8), byte(idx)}, uint64(idx+1))
		if _, found := reader.Lookup(key); !found {
			b.Fatal("key not found")
		}
	}
	b.StopTimer()
}

// BenchmarkBloomFilter measures MayContain on a Bloom filter with 100K keys.
func BenchmarkBloomFilter(b *testing.B) {
	logger.SetLevel(logger.ERROR)

	bf := NewBloomFilter(10000)
	keys := make([][]byte, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = []byte{byte(i >> 8), byte(i & 0xff)}
		bf.Add(keys[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bf.MayContain(keys[i%10000])
	}
	b.StopTimer()
}
