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

package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

var (
	benchSize        string
	benchIterations  int
	benchOp          string
	benchKeySize     int
	benchValueSize   int
	benchCF          string
	benchConcurrency int
)

func newBenchmarkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Run built-in performance benchmark",
		Long: `Run a performance benchmark against the connected ScoriaDB server.

Examples:
  # Quick benchmark (default: 10MB, 5 iterations)
  ./scoria-cli benchmark

  # Custom size and iterations
  ./scoria-cli benchmark --size=100MB --iterations=10

  # Benchmark specific operation
  ./scoria-cli benchmark --op=write --size=50MB
  ./scoria-cli benchmark --op=read --size=50MB
  ./scoria-cli benchmark --op=mixed --size=50MB

  # Parallel workers
  ./scoria-cli benchmark --concurrency=16

  # Custom key/value sizes
  ./scoria-cli benchmark --key-size=32 --value-size=4096

  # With Column Family
  ./scoria-cli benchmark --cf=mycf --size=50MB`,
		RunE: runBenchmark,
	}

	cmd.Flags().StringVar(&benchSize, "size", "10MB", "Total data size to write (KB, MB, GB)")
	cmd.Flags().IntVar(&benchIterations, "iterations", 5, "Number of benchmark runs")
	cmd.Flags().StringVar(&benchOp, "op", "mixed", "Operation: write, read, mixed")
	cmd.Flags().IntVar(&benchKeySize, "key-size", 16, "Key size in bytes")
	cmd.Flags().IntVar(&benchValueSize, "value-size", 256, "Value size in bytes")
	cmd.Flags().StringVar(&benchCF, "cf", "default", "Column Family name")
	cmd.Flags().IntVar(&benchConcurrency, "concurrency", 1, "Number of concurrent workers")

	return cmd
}

func runBenchmark(cmd *cobra.Command, args []string) error {
	totalBytes, err := parseSize(benchSize)
	if err != nil {
		return fmt.Errorf("invalid size: %w", err)
	}

	client, err := NewClient(addr, token)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	defer client.Close()

	fmt.Printf("🔬 ScoriaDB Benchmark\n")
	fmt.Printf("   Column Family: %s\n", benchCF)
	fmt.Printf("   Size: %s (%d bytes)\n", benchSize, totalBytes)
	fmt.Printf("   Iterations: %d\n", benchIterations)
	fmt.Printf("   Operation: %s\n", benchOp)
	fmt.Printf("   Key size: %d bytes\n", benchKeySize)
	fmt.Printf("   Value size: %d bytes\n", benchValueSize)
	fmt.Printf("   Concurrency: %d\n\n", benchConcurrency)

	var results []time.Duration

	for i := 0; i < benchIterations; i++ {
		var duration time.Duration
		var err error

		switch benchOp {
		case "write":
			duration, err = runWriteBenchmark(client, totalBytes)
		case "read":
			duration, err = runReadBenchmark(client, totalBytes)
		case "mixed":
			duration, err = runMixedBenchmark(client, totalBytes)
		default:
			return fmt.Errorf("unknown operation: %s (use write, read, or mixed)", benchOp)
		}

		if err != nil {
			return fmt.Errorf("benchmark iteration %d failed: %w", i+1, err)
		}

		results = append(results, duration)
		throughput := float64(totalBytes) / duration.Seconds()
		fmt.Printf("   Run %d: %.2f ms (%.2f MB/s)\n",
			i+1,
			float64(duration)/float64(time.Millisecond),
			throughput/(1024*1024),
		)
	}

	if len(results) == 0 {
		return fmt.Errorf("no benchmark results")
	}

	avg := avgDuration(results)
	throughput := float64(totalBytes) / avg.Seconds()

	fmt.Printf("\n📊 Results\n")
	fmt.Printf("   Average: %.2f ms\n", float64(avg)/float64(time.Millisecond))
	fmt.Printf("   Throughput: %.2f MB/s\n", throughput/(1024*1024))
	fmt.Printf("   Throughput: %.2f GB/s\n", throughput/(1024*1024*1024))

	opsPerRun := int(totalBytes) / (benchKeySize + benchValueSize)
	if opsPerRun > 0 {
		opsPerSecond := float64(opsPerRun) / avg.Seconds()
		fmt.Printf("   Operations: %.0f ops/s\n", opsPerSecond)
	}

	return nil
}

func runWriteBenchmark(client *Client, totalBytes int64) (time.Duration, error) {
	numOps := int(totalBytes) / (benchKeySize + benchValueSize)
	if numOps < 1 {
		numOps = 1
	}

	// Pre-generate keys and values
	keys := make([][]byte, numOps)
	values := make([][]byte, numOps)
	for i := 0; i < numOps; i++ {
		keys[i] = make([]byte, benchKeySize)
		values[i] = make([]byte, benchValueSize)
		for j := range keys[i] {
			keys[i][j] = byte((i + j) % 256)
		}
		for j := range values[i] {
			values[i][j] = byte((i + j + 128) % 256)
		}
	}

	ctx := context.Background()
	start := time.Now()

	if benchConcurrency <= 1 {
		// Sequential
		for i := 0; i < numOps; i++ {
			if _, err := client.Put(ctx, keys[i], values[i], benchCF); err != nil {
				return 0, fmt.Errorf("put failed at op %d: %w", i, err)
			}
		}
	} else {
		// Concurrent
		var wg sync.WaitGroup
		errCh := make(chan error, numOps)
		workCh := make(chan int, numOps)

		for w := 0; w < benchConcurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range workCh {
					if _, err := client.Put(ctx, keys[idx], values[idx], benchCF); err != nil {
						errCh <- err
						return
					}
				}
			}()
		}

		for i := 0; i < numOps; i++ {
			workCh <- i
		}
		close(workCh)
		wg.Wait()
		close(errCh)

		for err := range errCh {
			if err != nil {
				return 0, err
			}
		}
	}

	return time.Since(start), nil
}

func runReadBenchmark(client *Client, totalBytes int64) (time.Duration, error) {
	// Write data first
	_, err := runWriteBenchmark(client, totalBytes)
	if err != nil {
		return 0, fmt.Errorf("write phase failed: %w", err)
	}

	numOps := int(totalBytes) / (benchKeySize + benchValueSize)
	if numOps < 1 {
		numOps = 1
	}

	// Pre-generate keys
	keys := make([][]byte, numOps)
	for i := 0; i < numOps; i++ {
		keys[i] = make([]byte, benchKeySize)
		for j := range keys[i] {
			keys[i][j] = byte((i + j) % 256)
		}
	}

	ctx := context.Background()
	start := time.Now()

	if benchConcurrency <= 1 {
		for i := 0; i < numOps; i++ {
			if _, err := client.Get(ctx, keys[i], benchCF); err != nil {
				return 0, fmt.Errorf("get failed at op %d: %w", i, err)
			}
		}
	} else {
		var wg sync.WaitGroup
		errCh := make(chan error, numOps)
		workCh := make(chan int, numOps)

		for w := 0; w < benchConcurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range workCh {
					if _, err := client.Get(ctx, keys[idx], benchCF); err != nil {
						errCh <- err
						return
					}
				}
			}()
		}

		for i := 0; i < numOps; i++ {
			workCh <- i
		}
		close(workCh)
		wg.Wait()
		close(errCh)

		for err := range errCh {
			if err != nil {
				return 0, err
			}
		}
	}

	return time.Since(start), nil
}

func runMixedBenchmark(client *Client, totalBytes int64) (time.Duration, error) {
	numOps := int(totalBytes) / (benchKeySize + benchValueSize)
	if numOps < 2 {
		numOps = 2
	}
	halfOps := numOps / 2

	// Pre-generate keys and values
	keys := make([][]byte, halfOps)
	values := make([][]byte, halfOps)
	for i := 0; i < halfOps; i++ {
		keys[i] = make([]byte, benchKeySize)
		values[i] = make([]byte, benchValueSize)
		for j := range keys[i] {
			keys[i][j] = byte((i + j) % 256)
		}
		for j := range values[i] {
			values[i][j] = byte((i + j + 128) % 256)
		}
	}

	ctx := context.Background()
	start := time.Now()

	// Write phase (50%)
	for i := 0; i < halfOps; i++ {
		if _, err := client.Put(ctx, keys[i], values[i], benchCF); err != nil {
			return 0, fmt.Errorf("mixed write failed at op %d: %w", i, err)
		}
	}

	// Read phase (50%)
	for i := 0; i < halfOps; i++ {
		if _, err := client.Get(ctx, keys[i], benchCF); err != nil {
			return 0, fmt.Errorf("mixed read failed at op %d: %w", i, err)
		}
	}

	return time.Since(start), nil
}

func parseSize(s string) (int64, error) {
	var size int64
	var unit string

	if _, err := fmt.Sscanf(s, "%d%s", &size, &unit); err != nil {
		var raw int64
		if _, err := fmt.Sscanf(s, "%d", &raw); err == nil {
			return raw, nil
		}
		return 0, fmt.Errorf("failed to parse size: %s (use KB, MB, GB)", s)
	}

	switch unit {
	case "KB", "kb", "K", "k":
		return size * 1024, nil
	case "MB", "mb", "M", "m":
		return size * 1024 * 1024, nil
	case "GB", "gb", "G", "g":
		return size * 1024 * 1024 * 1024, nil
	default:
		return 0, fmt.Errorf("unknown unit: %s (use KB, MB, GB)", unit)
	}
}

func avgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	return sum / time.Duration(len(durations))
}
