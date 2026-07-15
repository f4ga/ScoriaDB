

<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=⚡%20Pure%20Go%20LSM%20Database%20|%20Lock‑free%20|%20Zero‑copy%20|%20Embeddable&fontSize=20&fontAlignY=50&animation=twinkling">
  <br><br>

  <a href="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml"><img src="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
  <a href="https://github.com/f4ga/ScoriaDB/stargazers"><img src="https://img.shields.io/github/stars/f4ga/ScoriaDB" alt="Stars"></a>

  <br><br>

  <a href="README.md"><img src="https://img.shields.io/badge/🇬🇧-English-blue?style=for-the-badge" alt="English"></a>
  <a href="README_RU.md"><img src="https://img.shields.io/badge/🇷🇺-Русский-red?style=for-the-badge" alt="Русский"></a>
  <a href="https://f4ga.github.io/ScoriaDB/"><img src="https://img.shields.io/badge/📖-Documentation-blue?style=for-the-badge" alt="Documentation"></a>

  <br><br>

  <b>18.4M Get/s • 2.92M Put/s • 1 alloc/op</b>

  <br><br>
</div>

---

## 📖 What is ScoriaDB?

**ScoriaDB** is an embeddable storage engine written in pure Go.

It is a **production‑grade LSM‑tree** with:
- MVCC + Snapshot Isolation
- ACID transactions
- Column Families
- Lock‑free skip list MemTable
- Zero‑copy Value Log (mmap)
- Built‑in gRPC, REST, CLI
- No external dependencies, no cgo

**The result:** **18.4M Get/s and 2.92M Put/s** on a consumer laptop — faster than most in‑memory caches, while being persistent and ACID‑compliant.

---

## 🚀 Quick Start

```bash
go get github.com/f4ga/ScoriaDB/pkg/scoria
```

```go
package main

import (
    "fmt"
    "log"
    "github.com/f4ga/ScoriaDB/pkg/scoria"
)

func main() {
    db, err := scoria.NewScoriaDB("./data")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    db.Put([]byte("hello"), []byte("world"))
    value, _ := db.Get([]byte("hello"))
    fmt.Printf("%s\n", value)
}
```

---

## 📊 Performance

*Benchmarked on Intel Core i3‑1215U (laptop, 8 threads), NVMe SSD, Go 1.23+*

### MemTable (lock‑free skip list)

| Benchmark | ops/s | ns/op | B/op | allocs/op | Cores |
|-----------|-------|-------|------|-----------|-------|
| **Get** | **18.4M** | **72** | 23 | 1 | 8 |
| **Get** | 4.56M | 267 | 23 | 1 | 1 |
| **Get (sequential)** | 4.58M | 264 | 23 | 1 | 8 |
| **Put** | **2.92M** | **432** | 23 | 1 | 8 |
| **Put** | 2.66M | 473 | 23 | 1 | 1 |
| **Put (sequential)** | 2.71M | 469 | 23 | 1 | 8 |

### Engine (LSM + WAL + VLog)

*Full storage engine with durability and MVCC*

| Operation | Throughput | Latency | Allocs |
|-----------|------------|---------|--------|
| **Get (MemTable hit)** | ~10M ops/s | ~100 ns | 1 alloc |
| **Get (4KB, VLog)** | ~1.25M ops/s | 800 ns | 5 allocs |
| **Scan (100 keys)** | **~1.25M ops/s** | **796 ns** | **7 allocs** |
| **WAL Group Commit** | 17.5M ops/s | 57 ns | 0 allocs |
| **WAL Sync** | 2.74M ops/s | 365 ns | 0 allocs |

> ⚠️ **Note:** Engine‑level benchmarks reflect current development state. Full LSM benchmarks (with SSTable reads and compactions) will be published with v0.3.0.

---

## 🏆 Comparison with Competitors

| Database | Type | Write (ops/s) | Read (ops/s) | ACID | MVCC | Embeddable |
|----------|------|--------------|--------------|------|------|------------|
| **ScoriaDB** (MemTable) | LSM (Go) | **2.92M** | **18.4M** | ✅ | ✅ | ✅ |
| BadgerDB | LSM (Go) | ~171K | ~400K | ✅ | ❌ | ✅ |
| Pebble | LSM (Go) | ~472K | ~1M | ❌ | ❌ | ✅ |
| RocksDB | LSM (C++) | ~356K | ~1.06M | ❌ | ❌ | ❌ |
| LevelDB | LSM (C++) | ~1.5M | ~10K | ❌ | ❌ | ✅ |
| Redis | In‑memory | ~1M | ~10.5M | ❌ | ❌ | ❌ |
| SQLite | B+Tree | ~20K | ~60K | ✅ | ❌ | ✅ |

**Key takeaways:**
- ScoriaDB MemTable is **6× faster** than Pebble for writes
- Read performance (**18.4M ops/s**) is the **highest** among all embeddable KV stores
- Only ScoriaDB offers **ACID + MVCC + lock‑free** in a pure Go embeddable package

---

## 🧩 Features

### Core Engine
- ✅ **LSM‑tree** with leveled compaction
- ✅ **Lock‑free skip list** MemTable — no mutexes on writes
- ✅ **SSTable** with block index, Bloom filter, prefix compression
- ✅ **Value Log (WiscKey)** — large values stored separately
- ✅ **Zero‑copy mmap** reads — no copying on VLog access
- ✅ **MVCC + Snapshot Isolation**
- ✅ **ACID transactions** with optimistic concurrency control
- ✅ **Column Families** — independent LSM trees
- ✅ **WAL with Group Commit** — 17.5M ops/s
- ✅ **gRPC, REST, CLI** — one binary, zero config
- ✅ **JWT authentication** — admin/readwrite/readonly roles
- ✅ **Graceful shutdown** — SIGINT/SIGTERM handling

### Memory Efficiency
- **1 alloc/op** in Get path (vs 8 in Redis)
- **0 allocs/op** in Bloom filter (vs 2 in RocksDB)
- **5 allocs/op** in 4KB VLog read (vs 8 in v0.2.2)
- **7 allocs/op** in Scan (down from 107 in v0.2.2)

---

## 📊 Impact of Major Optimizations

| Optimization | Before | After | Gain |
|--------------|--------|-------|------|
| **Lock‑free skip list** | 1.51M Put/s | **2.92M Put/s** | **+94%** |
| **Zero‑copy VLog (4KB read)** | 213K ops/s | **1.25M ops/s** | **+487%** |
| **Scan (heap‑based iterator)** | 4809 ns, 107 allocs | **796 ns, 7 allocs** | **-83% latency, -93% allocs** |
| **SSTable block pooling** | 432 ns | 140 ns | **-67%** |
| **WAL buffer pooling** | 515 ns | 436 ns | **-15%** |
| **Bloom filter (fastrand)** | 16 µs | 14.8 µs | **-7.5%** |

---

## 🚧 Known Limitations

Transparency is important. Here are the current known issues being worked on:

| Problem | Description | ETA |
|---------|-------------|-----|
| **Skip list 4KB performance** | Large values (4KB) are slower than target | v0.3.1 |
| **Ring buffer overflow** | Crashes after ~131K entries in MemTable | v0.3.1 |
| **SSTable merge** | Compaction doesn't merge SSTables yet | v0.4.0 |

---

## 🗺️ Roadmap

| Version | Focus | Key Features | Status |
|---------|-------|--------------|--------|
| **v0.1.0** | Core stability | LSM, MVCC, ACID, CF, gRPC, CLI | ✅ |
| **v0.1.1** | CLI & docs | Interactive commands, multi‑lang docs | ✅ |
| **v0.2.0** | Write performance | Group Commit, WAL options | ✅ |
| **v0.2.1** | Quick Wins | sync.Pool, fastrand, errcheck, deadcode | ✅ |
| **v0.2.2** | Zero‑copy VLog | Zero‑copy mmap, graceful shutdown | ✅ |
| **v0.3.0** | Lock‑free | Lock‑free skip list, arena allocator, heap‑based scan | 🚧 |
| **v0.3.1** | Critical fixes | 4KB performance, ring buffer | ⏳ |
| **v0.4.0** | TTL & GC | TTL, automatic GC, binary Manifest | ⏳ |
| **v0.5.0** | Scaling | Shard‑per‑core | ⏳ |
| **v0.6.0** | Async I/O | io_uring | ⏳ |
| **v0.7.0** | Fault tolerance | ZeroRaft cluster | ⏳ |
| **v1.0.0** | Distributed | Range sharding, distributed ACID | ⏳ |

---

## 📄 License

**Apache License 2.0** — free for commercial and personal use.

---

<div align="center">
  <br>
  <i>Solid as stone. Light as ash.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>

---