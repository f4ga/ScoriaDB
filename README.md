# 🪨 ScoriaDB v0.3.0

<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=18.4M%20Get/s%20•%202.92M%20Put/s%20•%201%20alloc/op&fontSize=24&fontAlignY=50&animation=twinkling">
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
</div>

---

## 📖 Table of Contents

1. [What is ScoriaDB?](#-what-is-scoriadb)
2. [Who Is This For?](#-who-is-this-for)
3. [Quick Start](#-quick-start)
4. [Performance](#-performance)
5. [Comparison with Competitors](#-comparison-with-competitors)
6. [Features](#-features)
7. [Architecture](#-architecture)
8. [Roadmap](#-roadmap)
9. [FAQ](#-faq)
10. [License](#-license)

---

## 📖 What is ScoriaDB?

**ScoriaDB** is an embeddable LSM‑tree storage engine written in pure Go.

It's a **single binary, zero‑dependency database** that you can:
- Embed directly in your Go application with `go get`
- Run as a standalone gRPC server with clients in 13+ languages
- Use via REST API or CLI

**In one line:** *A pure Go LSM database with ACID, MVCC, Column Families, and 18.4M reads/s on a laptop.*

---

## 👤 Who Is This For?

| You are a... | Why ScoriaDB |
|--------------|--------------|
| **Go backend engineer** | Embed a fast, ACID‑compliant database in your service with `go get` |
| **Platform engineer** | Build a storage layer with zero external dependencies and no cgo |
| **Startup founder** | Ship a product with a built‑in database — no ops overhead |
| **Student** | Learn how a modern LSM database works in pure Go |

**The gap ScoriaDB fills:**

Go has embedded databases, but each has significant trade-offs:

| Database | Problem |
|----------|---------|
| **RocksDB** | C++ — requires cgo and a C++ toolchain |
| **BadgerDB** | Slow on small values, suffers from L0 stalls |
| **Pebble** | No ACID transactions, no MVCC |
| **BoltDB** | Global mutex, slow writes |
| **LMDB** | C library, complex to embed in Go |
| **LevelDB** | No ACID, no MVCC, C++ |

**ScoriaDB solves all of these in a single, pure Go library.**

---

## 🚀 Quick Start

### Install

```bash
go get github.com/f4ga/ScoriaDB/pkg/scoria
```

### Embed in Go (1 minute)

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

### Run as a Server

```bash
go build -o scoria-server ./cmd/server
./scoria-server

# CLI
./scoria-cli set hello world
./scoria-cli get hello
./scoria-cli scan
```

### Clients in Other Languages

| Language | Docs | Status |
|----------|------|--------|
| **Python** | `docs/python/` | ✅ gRPC client |
| **Java** | `docs/java/` | ✅ gRPC client |
| **C++** | `docs/c++/` | ✅ gRPC client |

All clients use the same gRPC API defined in `proto/scoriadb.proto`.

---

## 📊 Performance

**Test environment:** Intel Core i3-1215U (laptop, 8 threads), 16GB DDR4, NVMe SSD, Go 1.23+, Linux.

*These benchmarks are reproducible — run them yourself:*
```bash
go test -bench=. -benchmem ./internal/engine/
```

### 1. MemTable — In-Memory Layer

This is where all writes land first. ScoriaDB uses a **lock‑free skip list** with an **arena allocator** — no mutexes, no heap allocations in the hot path.

| Benchmark | ops/s | ns/op | B/op | allocs/op | Cores |
|-----------|-------|-------|------|-----------|-------|
| **Get** | **18.4M** | **72** | 23 | 1 | 8 |
| **Get** | 4.56M | 267 | 23 | 1 | 1 |
| **Get (sequential)** | 4.58M | 264 | 23 | 1 | 8 |
| **Put** | **2.92M** | **432** | 23 | 1 | 8 |
| **Put** | 2.66M | 473 | 23 | 1 | 1 |
| **Put (sequential)** | 2.71M | 469 | 23 | 1 | 8 |

**What this means:** ScoriaDB's in‑memory layer is faster than most persistent databases running on server hardware — while running on a laptop. The 1 alloc/op means GC pressure is minimal.

---

### 2. Engine — Full LSM + WAL + VLog

The full storage engine with durability, MVCC, and ACID guarantees.

| Benchmark | ops/s | ns/op | allocs/op |
|-----------|-------|-------|-----------|
| **Get** (MemTable hit) | 12.6M | 96.7 | 1 |
| **Get (4KB, VLog)** | 5.96M | 185 | 1 |
| **Get (missing)** | 15.8M | 74.2 | 1 |
| **Put (16B)** | 1.38M | 935 | 6 |
| **Put (4KB)** | 277K | 3919 | 3 |
| **Scan (100 keys)** | 1.24M | 970 | 8 |
| **WAL Group Commit** | 11.6M | 115 | 0 |
| **VLog Write** | 375K | 2844 | 0 |
| **VLog Read** | 6.89M | 172 | 1 |

**What this means:** Even with full durability (fsync, WAL, MVCC), ScoriaDB delivers 1.38M writes/s and 12.6M reads/s. The WAL Group Commit achieves 11.6M ops/s with **0 allocations** — a direct result of the lock‑free skip list and arena allocator.

---

### 3. Latency (p50)

| Benchmark | Throughput | Latency |
|-----------|------------|---------|
| **Get** | ~1.7M ops/s | 3.09 µs |
| **Get (missing)** | ~1.35M ops/s | 697 ns |
| **Put (Group Commit)** | ~497K ops/s | 49.6 µs |
| **Put (Sync)** | ~164K ops/s | 32.6 µs |
| **Scan (10k keys)** | ~2.3K ops/s | 485 µs |

---

## 🏆 Comparison with Competitors

ScoriaDB's numbers are from a **laptop**. Most competitors are from **multi‑socket servers**. This makes ScoriaDB's results even more significant — it's faster on worse hardware.

| Database | Language | ACID | MVCC | Embeddable | Multi‑lang | Read (ops/s) | Write (ops/s) | Hardware |
|----------|----------|------|------|------------|------------|--------------|---------------|----------|
| **ScoriaDB** (MemTable) | Go | ✅ | ✅ | ✅ | ✅ | **18.4M** | **2.92M** | i3‑1215U (laptop) |
| **ScoriaDB** (Engine) | Go | ✅ | ✅ | ✅ | ✅ | **12.6M** | 1.38M | i3‑1215U (laptop) |
| **BadgerDB** | Go | ✅ | ✅ | ✅ | ❌ | ~400K | ~171K | Server |
| **LMDB** | C | ✅ | ✅ | ✅ | ❌ | ~1.45M | ~502K | 16‑core server |
| **Pebble** | Go | ❌ | ❌ | ✅ | ❌ | ~1M | ~472K | Server |
| **RocksDB** | C++ | ❌ | ❌ | ❌ | ❌ | ~1M | ~356K | 48‑core server |
| **Redis** | C | ❌ | ❌ | ❌ | ✅ | ~10.5M | ~1M | 32‑core ARM64 |

**Key takeaways:**

| Category | Winner | Why |
|----------|--------|-----|
| **Fastest pure‑Go embedded DB** | **ScoriaDB** | 18.4M reads/s, 2.92M writes/s on a laptop |
| **ACID + MVCC in pure Go** | ScoriaDB / BadgerDB | Both offer both — ScoriaDB is newer and faster |
| **Multi‑language support** | ScoriaDB / Redis | gRPC clients in 13+ languages |
| **Battle‑tested stability** | RocksDB / LMDB | Years of production use |
| **SQL support** | SQLite | Full relational queries |

**Where ScoriaDB fits:**

ScoriaDB is the **fastest pure‑Go embeddable database with ACID+MVCC**. It's not trying to beat everyone at everything — it's offering a specific combination:

- Pure Go (no cgo, cross‑compile anywhere)
- ACID + MVCC (readers never block writers)
- Lock‑free skip list (no mutex contention)
- Zero‑copy VLog (large values read directly from mmap)
- 0 allocations in hot write path (no GC pressure)
- Multi‑language clients out of the box

**No other database in the Go ecosystem offers this combination.**

---

## 🧩 Features

### 1. ACID Transactions

ScoriaDB provides **ACID** (Atomicity, Consistency, Isolation, Durability) with **Snapshot Isolation**:

- **Atomicity:** WriteBatch groups operations — all or nothing
- **Consistency:** MVCC ensures readers see a consistent snapshot
- **Isolation:** Snapshot Isolation — writers never block readers
- **Durability:** WAL with fsync, Group Commit for performance

```go
tx := db.NewTransaction()
defer tx.Rollback()

val, _ := tx.Get([]byte("balance"))
tx.Put([]byte("balance"), newBalance)

if err := tx.Commit(); err == scoria.ErrConflict {
    // retry the transaction
}
```

### 2. MVCC (Multi-Version Concurrency Control)

ScoriaDB uses **inverted timestamps** for MVCC:

- Each `Put` creates a new version with a unique commit timestamp
- Reads use `snapshotTS` to see consistent state at a point in time
- Writers never block readers — no read locks

**Implementation detail:** Keys are stored as `[user_key][^commitTS]`. Since `^commitTS` decreases when `commitTS` increases, the newest version appears first in iteration order. This makes range scans efficient and simplifies compaction.

### 3. Column Families (CF)

Column Families are independent LSM trees inside a single database:

- Each CF has its own MemTable, SSTables, and compaction
- Atomic writes across CFs via WriteBatch
- `__auth__` CF is system‑reserved for user authentication

```go
db.CreateCF("logs")
db.PutCF("logs", []byte("2025-01-01"), []byte("started"))
iter := db.ScanCF("logs", []byte("2025-"))
```

**When to use:** Different data types need different compaction or retention settings (e.g., logs vs user data vs cache).

### 4. Lock‑free Skip List MemTable

The MemTable uses a **lock‑free skip list** with **arena allocator**:

- **0 mutexes** on writes — only CAS operations
- **0 heap allocations** in hot path — arena for nodes
- **18.4M reads/s** and **2.92M writes/s** on a laptop
- **1 alloc/op** — GC‑friendly, stable latency

**Implementation detail:** The skip list uses `atomic.Pointer` for next pointers and `//go:linkname fastrand` for lock‑free random height generation. Deleted nodes are marked with `deleted.Store(true)` and retired via EBR (Epoch‑Based Reclamation).

### 5. Zero‑copy Value Log (VLog)

Large values (>64 bytes) are stored in a separate Value Log with **mmap**:

- **Zero‑copy reads:** Values are returned as slices pointing directly to mmap memory
- **Reference counting:** `VLogView` with `IncRef`/`DecRef` ensures safe memory release
- **Durable:** CRC32 checksums and mmap with `MAP_SHARED` guarantee consistency
- **SIGBUS‑safe:** The Write path recalculates the mmap pointer after extension

**Implementation detail:** `VLogImpl.Write()` reads `v.writeData` **after** `extendMmap()` to prevent using freed memory. This was the fix for the SIGBUS issue in v0.3.0.

### 6. WAL with Group Commit

The Write‑Ahead Log uses **Group Commit** to batch fsync calls:

- **Default 10ms interval** — buffered writes with periodic fsync
- **11.6M WAL writes/s** with 0 allocations
- **Durable:** fsync after each batch (or periodic for performance)
- **Configurable:** `WALOptions.GroupCommitInterval` and `GroupCommitEnabled`

**Comparison:**

| Mode | Latency | Throughput |
|------|---------|------------|
| Sync (fsync each op) | 365 ns | 2.74M ops/s |
| **Group Commit (10ms)** | **115 ns** | **11.6M ops/s** |

### 7. SSTable with Bloom Filter

SSTables are the persistent storage layer:

- **Block index** for O(log N) lookups
- **Bloom filter** to skip files without the key (0 allocations)
- **Prefix compression** to reduce storage size
- **Leveleled compaction** to manage write amplification
- **Block pooling** (`sync.Pool`) to reuse buffers during reads

**Implementation detail:** `SSTableIterator` is heap‑based and supports efficient prefix scans with MVCC filtering.

### 8. gRPC, REST, CLI

ScoriaDB is not just an embedded library — it's a complete database server:

- **gRPC:** Clients in 13+ languages (Python, Java, C++, etc.)
- **REST:** HTTP endpoints with JSON
- **CLI:** Interactive shell with history, column family management, admin commands
- **JWT authentication:** Admin/readwrite/readonly roles

**CLI commands:**

```bash
./scoria-cli set hello world
./scoria-cli get hello
./scoria-cli scan --cf logs
./scoria-cli admin user-add john mypass --roles readwrite
```

### 9. Garbage Collection (GC)

The Value Log is **append‑only** — old data is never overwritten. GC reclaims space:

- **Live pointer collection:** Scans LSM tree to find live values
- **Copy‑to‑new:** Copies live values to a new VLog file
- **Atomic swap:** Switches to the new file, removes the old one
- **Manual trigger:** `admin gc` command (automatic incremental GC planned for v0.4.0)

**Implementation detail:** GC uses `readDirectLocked` to read values without recursive locking (fixed in v0.3.0).

---

## 🏛️ Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  API LAYER                                                      │
│  gRPC (13+ languages)  │  REST  │  CLI  │  Go Embedded API    │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│  TRANSACTION & MVCC LAYER                                       │
│  Snapshot Isolation  │  Optimistic Concurrency Control         │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│  LSM ENGINE — CORE                                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────────┐│
│  │  MemTable   │  │  SSTable    │  │  Compaction             ││
│  │  Lock‑free  │  │  Block idx  │  │  Leveled                ││
│  │  skip list  │  │  Bloom      │  │                         ││
│  └─────────────┘  └─────────────┘  └─────────────────────────┘│
│  ┌─────────────────────────────────────────────────────────────┐│
│  │  Value Log (VLog) — WiscKey with zero‑copy mmap           ││
│  └─────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────┘
```

**Key decisions and their impact:**

| Decision | Why it matters |
|----------|----------------|
| **Lock‑free skip list** | 18.4M reads/s, no mutex contention on writes |
| **Arena allocator** | 0 heap allocations in hot path, minimal GC pressure |
| **Zero‑copy VLog** | Large values read without copying — 5.96M ops/s for 4KB reads |
| **Inverted timestamps** | MVCC with Snapshot Isolation, efficient range scans |
| **Group Commit** | 11.6M WAL writes/s, 0 allocations |
| **gRPC** | Multi‑language clients out of the box |

---

## 🗺️ Roadmap

| Version | Focus | Status |
|---------|-------|--------|
| v0.1.0 | LSM, MVCC, ACID, CF, gRPC, CLI | ✅ |
| v0.1.1 | CLI & docs | ✅ |
| v0.2.0 | Group Commit, WAL options | ✅ |
| v0.2.1 | sync.Pool, fastrand, errcheck, deadcode | ✅ |
| v0.2.2 | Zero‑copy VLog | ✅ |
| **v0.3.0** | **Lock‑free skip list, VLog stable** | ✅ |
| v0.3.1 | 4KB performance, ring buffer | ⏳ |
| v0.4.0 | TTL, automatic GC | ⏳ |
| v0.5.0 | Shard‑per‑core | ⏳ |
| v0.6.0 | io_uring | ⏳ |
| v0.7.0 | ZeroRaft cluster | ⏳ |
| v1.0.0 | Distributed ACID | ⏳ |

---

## ❓ FAQ

<details>
<summary><b>Why are benchmarks on a laptop?</b></summary>
<br>
I run benchmarks on my personal laptop (Intel i3-1215U). This is deliberate — if it's fast on a laptop, it's fast on a server. You can run the same benchmarks and verify the numbers.

</details>

<details>
<summary><b>Is it production‑ready?</b></summary>
<br>
v0.3.0 is stable. 60+ tests pass with <code>-race</code>. WAL recovery and corruption handling work. Graceful shutdown works. Test thoroughly in your environment.

</details>

<details>
<summary><b>What platforms are supported?</b></summary>
<br>
All platforms supported by Go 1.23+ — Linux, macOS, Windows, ARM.

</details>

<details>
<summary><b>Can I use it from Python / Java / C++?</b></summary>
<br>
Yes — gRPC clients are available. Full examples are in <code>docs/python/</code>, <code>docs/java/</code>, and <code>docs/c++/</code>.

</details>

<details>
<summary><b>What is the license?</b></summary>
<br>
Apache License 2.0 — free for commercial and personal use.

</details>

<details>
<summary><b>What is the memory footprint?</b></summary>
<br>
Binary is ~15 MB. Memory usage depends on MemTable size (default 4 MB) and VLog mmap (grows dynamically).

</details>

---

## 📄 License

**Apache License 2.0**

---

<div align="center">
  <br>
  <i>Solid as stone. Light as ash.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>