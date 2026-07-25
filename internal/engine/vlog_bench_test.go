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
	"os"
	"testing"

	"github.com/f4ga/ScoriaDB/internal/engine/vfs"
	"github.com/f4ga/ScoriaDB/internal/logger"
)

// BenchmarkVLogWrite measures writing a 4KB value to the Value Log.
// Zero syscalls in hot path (mmap write), except first call which extends mmap.
//
// NOTE: Temporarily reduces VLogExtendSize to 64MB for benchmarking.
// This prevents disk exhaustion and SIGBUS during long benchmark runs.
// The original value is restored after the benchmark completes.
func BenchmarkVLogWrite(b *testing.B) {
	logger.SetLevel(logger.ERROR)

	// Reduce mmap size for benchmark to prevent disk exhaustion.
	// 64MB is enough for ~16K entries of 4KB each.
	// The original value is restored after the benchmark completes.
	origExtendSize := VLogExtendSize
	VLogExtendSize = 64 * 1024 * 1024 // 64MB instead of 512MB
	defer func() { VLogExtendSize = origExtendSize }()

	dir, err := os.MkdirTemp("", "vlog-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	vlog, err := OpenVLog(vfs.Default, dir+"/vlog.db")
	if err != nil {
		b.Fatal(err)
	}
	defer vlog.Close()

	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := vlog.Write(value); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
}

// BenchmarkVLogRead measures reading a 4KB value from the Value Log via ReadView (zero-copy).
func BenchmarkVLogRead(b *testing.B) {
	logger.SetLevel(logger.ERROR)

	dir, err := os.MkdirTemp("", "vlog-bench-*")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(dir)

	vlog, err := OpenVLog(vfs.Default, dir+"/vlog.db")
	if err != nil {
		b.Fatal(err)
	}
	defer vlog.Close()

	value := make([]byte, 4096)
	for i := range value {
		value[i] = byte(i % 256)
	}

	vp, err := vlog.Write(value)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		view, err := vlog.ReadView(vp)
		if err != nil {
			b.Fatal(err)
		}
		view.Release()
	}
	b.StopTimer()
}
