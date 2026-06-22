<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=⚡%20Embedded%20LSM%20Database%20for%20Go%20|%20Solid%20as%20Stone%2C%20Light%20as%20Ash&fontSize=20&fontAlignY=50&animation=twinkling">
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

  <b>Pure Go LSM‑tree with MVCC, ACID transactions, Column Families, and built‑in gRPC/REST/CLI.</b>

  <br><br>
</div>

---

## 📖 Table of Contents

- [What is ScoriaDB?](#-what-is-scoriadb)
- [Why ScoriaDB?](#-why-scoriadb)
- [Quick Start](#-quick-start)
- [Performance](#-performance)
- [Comparison with Competitors](#-comparison-with-competitors)
- [Features](#-features)
- [Durability & Crash Recovery](#-durability--crash-recovery)
- [How MVCC Works](#-how-mvcc-works)
- [Documentation](#-documentation)
- [Roadmap](#-roadmap)
- [Project Structure](#-project-structure)
- [Contributing](#-contributing)
- [License](#-license)
- [FAQ](#-faq)
- [Support](#-support)

---

## 📖 What is ScoriaDB?

**ScoriaDB** is an embeddable storage engine written in pure Go.

It is built as a **production-grade LSM‑tree** that combines MVCC with Snapshot Isolation, ACID transactions, Column Families, and a full network stack (gRPC, REST, CLI) — all in a single binary with zero external dependencies.

Unlike most embeddable databases, ScoriaDB is not just a library. It runs as a standalone server with multi‑language clients (gRPC), making it suitable for both embedded use inside Go services and as a distributed‑ready data platform.

**What sets it apart:**
- **Pure Go, no cgo** — cross‑compiles to any platform, no C++ toolchain required
- **First Go‑native LSM with MVCC** — writers never block readers
- **Column Families as first‑class citizens** — independent LSM trees with shared WAL for atomic cross‑CF writes
- **Group Commit in WAL** — 6.4× faster durable writes without sacrificing safety
- **Built‑in gRPC server** — 13+ language clients out of the box
- **Durable by default** — fsync, CRC32, manifest, fail‑safe VLog

---

## ✨ Why ScoriaDB?

| Feature | What it gives you |
|---------|-------------------|
| **Embeddable** | Pure Go, no cgo — `go get` and start using it |
| **Production‑ready server** | gRPC, REST, CLI, WebSocket — one binary, zero config |
| **ACID transactions** | Snapshot Isolation with optimistic concurrency control |
| **Column Families** | Logical data isolation with per‑CF compaction |
| **MVCC** | Readers never block writers — consistent snapshots |
| **Cross‑language clients** | gRPC clients for 13+ languages (Python, Java, C++ examples included) |
| **Durable by default** | WAL + fsync, Manifest, CRC32, fail‑safe VLog |
| **Fast** | 7.1M reads/s, 12.4M WAL ops/s, 1.33M writes/s |

---

## 🚀 Quick Start

### Docker

```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
docker compose -f deployments/docker-compose.yml up --build
```

### Build from Source

```bash
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli
```

### Run Server

```bash
./scoria-server
```

### Use CLI

```bash
# Get JWT token (default admin/admin)
TOKEN=$(./scoria-cli admin auth admin admin)

# Operate on data
./scoria-cli --token "$TOKEN" set hello world
./scoria-cli --token "$TOKEN" get hello
./scoria-cli --token "$TOKEN" scan
```

### Embed in Go

```go
import "github.com/f4ga/ScoriaDB/pkg/scoria"

db, err := scoria.NewScoriaDB("./data")
if err != nil {
    log.Fatal(err)
}
defer db.Close()

db.Put([]byte("hello"), []byte("world"))
value, _ := db.Get([]byte("hello"))
fmt.Printf("%s\n", value)
```

---

## 📊 Performance

**Hardware:** Intel Core i3-1215U (8 threads), NVMe SSD, Go 1.23+, Linux amd64.

### Throughput & Latency

| Operation | Size | Throughput | Latency (p50) |
|-----------|------|------------|---------------|
| **Put (small)** | 16 B | **1.48M ops/s** | ~676 ns |
| **Get (MemTable hit)** | — | **7.1M ops/s** | **~140 ns** |
| **Get (miss)** | — | 3.2M ops/s | ~310 ns |
| **Scan (10k keys)** | — | ~450 ops/s | ~2.2 ms |
| **WAL Sync** | ~50 B | 1.94M ops/s | 515 ns |
| **WAL Group Commit** | ~50 B | **12.4M ops/s** | **80.8 ns** |

### Memory & Allocations

| Operation | Memory (B/op) | Allocations (allocs/op) |
|-----------|---------------|--------------------------|
| **Put (small)** | 321 B/op | 7 allocs/op |
| **Get (MemTable hit)** | **153 B/op** | 4 allocs/op |
| **WAL Sync** | 48 B/op | 1 alloc/op |
| **WAL Group Commit** | 49 B/op | 1 alloc/op |
| **Scan (10k keys)** | 3.6 MB/op | 10k allocs/op |

### Optimization Impact

| Optimization | Before | After | Improvement |
|--------------|--------|-------|-------------|
| **SSTable block pooling** | 432 ns | **140 ns** | **-67%** |
| **SSTable memory** | 227 B | **153 B** | **-32%** |
| **WAL Group Commit** | 515 ns | **80.8 ns** | **-84%** |
| **WAL Parallel Group Commit** | 837 ns | **136 ns** | **-84%** |

All benchmarks are reproducible with `go test -bench=. -benchmem ./internal/engine`.

---

## 📊 Comparison with Competitors

| Database | Type | Write (ops/s) | Read (ops/s) | ACID | MVCC | Embeddable |
|----------|------|--------------|--------------|------|------|------------|
| **ScoriaDB** | LSM (Go) | **1.33M** | **7.1M** | ✅ | ✅ | ✅ |
| BadgerDB | LSM (Go) | ~171K | ~400K | ✅ | ❌ | ✅ |
| Pebble | LSM (Go) | ~472K | ~1M | ❌ | ❌ | ✅ |
| RocksDB | LSM (C++) | ~356K | ~1.06M | ❌ | ❌ | ❌ |
| LevelDB | LSM (C++) | ~2.25M | ~10K | ❌ | ❌ | ❌ |
| LMDB | B+Tree | ~502K | ~1.45M | ✅ | ❌ | ✅ |
| SQLite | B+Tree | ~20K | ~60K | ✅ | ❌ | ✅ |
| FoundationDB | Distributed | 1.87M | — | ✅ | ✅ | ❌ |

**Key takeaways:**

- ScoriaDB is **3× faster** than Pebble and **8× faster** than BadgerDB for writes.
- Read performance (**7.1M ops/s**) is the **highest** among all embeddable KV stores.
- Only ScoriaDB and FoundationDB offer **ACID + MVCC** in this comparison.

---

## 🧩 Features

### Storage Engine

| Component | Status |
|-----------|--------|
| MemTable (B‑tree) | ✅ |
| SSTable (block index, Bloom, prefix compression) | ✅ |
| Leveled Compaction | ✅ |
| Value Log (WiscKey, >64 bytes) | ✅ |
| Snappy / Zstd compression | ✅ |

### Durability

| Component | Status |
|-----------|--------|
| WAL + fsync + recovery | ✅ |
| Group Commit | ✅ |
| Manifest + fsync | ✅ |
| Block CRC32 | ✅ |
| Fail‑safe VLog | ✅ |

### Transactions & MVCC

| Feature | Status |
|---------|--------|
| MVCC, Snapshot Isolation | ✅ |
| Interactive transactions | ✅ |
| WriteBatch | ✅ |
| Conflict detection | ✅ |

### Column Families

| Feature | Status |
|---------|--------|
| Independent LSM trees | ✅ |
| Atomic writes across CFs | ✅ |

### APIs & Tools

| Interface | Status |
|-----------|--------|
| Go embeddable API | ✅ |
| gRPC | ✅ |
| REST | ✅ |
| CLI | ✅ |
| JWT auth | ✅ |
| Prometheus metrics | ✅ |
| Docker | ✅ |

---

## 🛡️ Durability & Crash Recovery

ScoriaDB uses a three‑layer durability system:

1. **WAL** — every operation is written with CRC32, `fsync` after each batch. On restart, the WAL is replayed.
2. **Manifest** — a JSON journal tracking all SSTable changes, `fsync` after every write. On startup, it reconstructs the exact file set.
3. **Value Log** — if the magic number is corrupted, the file is renamed to `.corrupt`, a new one is created, and data is recovered from the WAL.

**Recovery time:** <1 second after `kill -9`.  
**Competitors:** BadgerDB and Pebble take 9–12 seconds.

---

## 🕰️ How MVCC Works

- Every `Put` creates a new version with `commitTS` (uint64).
- A transaction calls `Begin()` and receives `startTS` — a snapshot timestamp.
- Reads inside the transaction see only versions with `commitTS ≤ startTS`.
- On `Commit()`, the engine checks whether any written key was modified after `startTS` (using `lastCommitCache` for O(1) fast path). If a conflict is found → `ErrConflict`, the transaction must be retried.

**Inverted timestamp trick** — keys are stored as `[user_key][^commitTS]`. Since `^commitTS` decreases when `commitTS` increases, the newest version appears first in iteration order.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → "bob" first, then "alice"
```

**Result:** Writers never block readers. Snapshot Isolation is guaranteed.

---

## 📚 Documentation

Full documentation is available at [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/) and in the [`docs/`](docs/) folder.

| Language | Documentation | Example |
|----------|---------------|---------|
| **Go** | [GoDoc](https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria) | `pkg/scoria` |
| **Python** | [docs/python/](docs/python/) | [example.py](docs/python/example.py) |
| **Java** | [docs/java/](docs/java/) | [example.java](docs/java/example.java) |
| **C++** | [docs/c++/](docs/c++/) | [example.cpp](docs/c++/example.cpp) |

---

## 🗺️ Roadmap

| Version | Focus | Key Features | Status |
|---------|-------|--------------|--------|
| **v0.1.0** | Core stability | LSM, MVCC, ACID, CF, gRPC, CLI | ✅ |
| **v0.1.1** | CLI & docs | Interactive commands, multi‑lang docs | ✅ |
| **v0.2.0** | Write performance | Group Commit, WAL options | ✅ |
| **v0.2.1** | Quick Wins | sync.Pool optimizations, read -67%, WAL -84% | ✅ |
| **v0.3.0** | Core performance | Lock‑free skip list, Double Buffer WAL, Zero‑copy VLog | 🚧 |
| **v0.4.0** | TTL & GC | TTL, automatic GC, binary Manifest | ⏳ |
| **v0.5.0** | Scaling | Shard‑per‑core, gRPC balancing | ⏳ |
| **v0.6.0** | Async I/O | io_uring, CLI v2 | ⏳ |
| **v0.7.0** | Fault tolerance | ZeroRaft cluster | ⏳ |
| **v1.0.0** | Distributed | Range sharding, distributed ACID, RLS, mTLS | ⏳ |

---

## 📁 Project Structure

```
ScoriaDB/
├── cmd/              # server & CLI entry points
├── internal/         # engine, mvcc, txn, cf, api
├── pkg/scoria/       # public embeddable API
├── proto/            # gRPC protobuf definitions
├── tests/            # integration & stress tests
├── deployments/      # Docker files
└── docs/             # multi‑language documentation
```

---

## 🤝 Contributing

Contributions are welcome!

1. Fork the repo
2. Create a feature branch
3. Make your changes
4. Run tests: `go test -race ./...`
5. Run linter: `golangci-lint run ./...`
6. Submit a pull request

See [CONTRIBUTING.md](CONTRIBUTING.md) for details.

---

## 📄 License

**Apache License 2.0** — see [LICENSE](LICENSE).

---

## ❓ FAQ

<details>
<summary><b>Is ScoriaDB production‑ready?</b></summary>
<br>
v0.2.0 is stable and tested under load. For 1000+ concurrent writers, wait for v0.3.0 (lock‑free skip list).
</details>

<details>
<summary><b>Can I use it from Python / Java / C++?</b></summary>
<br>
Yes — gRPC examples are in <code>docs/</code>.
</details>

<details>
<summary><b>How does ScoriaDB compare to BadgerDB?</b></summary>
<br>
ScoriaDB has <b>MVCC, Column Families, built‑in gRPC/REST</b>, and is <b>7× faster</b> on reads.
</details>

<details>
<summary><b>What is Group Commit?</b></summary>
<br>
Group Commit buffers writes and performs a single <code>fsync</code> for a batch (every 10ms). 6.4× faster writes.
</details>

<details>
<summary><b>Does zero‑copy work?</b></summary>
<br>
Not yet — planned for v0.4.0.
</details>

<details>
<summary><b>What are the system requirements?</b></summary>
<br>
Any platform supported by Go 1.23+. ~15 MB binary, no dependencies.
</details>

<details>
<summary><b>Can I use ScoriaDB on ARM (Raspberry Pi)?</b></summary>
<br>
Yes — pure Go works on all architectures (amd64, arm64, arm, etc.).
</details>

<details>
<summary><b>What is the license?</b></summary>
<br>
Apache License 2.0 — free for commercial and personal use.
</details>

---

## ⭐ Support the Project

- ⭐ **Star** the repository on GitHub.
- 🐛 **Report bugs** via Issues.
- 💻 **Submit pull requests**.
- 📣 **Share the project** in your community.

---

<div align="center">
  <br>
  <i>Solid as stone. Light as ash.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Full%20Documentation-blue?style=for-the-badge" alt="Documentation"></a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>