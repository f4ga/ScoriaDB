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

- [📖 What is ScoriaDB?](#-what-is-scoriadb)
- [✨ Why ScoriaDB?](#-why-scoriadb)
- [🚀 Quick Start](#-quick-start)
  - [Docker](#docker)
  - [Build from Source](#build-from-source)
  - [Run Server](#run-server)
  - [Use CLI](#use-cli)
  - [Embed in Go](#embed-in-go)
- [📊 Benchmarks](#-benchmarks)
  - [Group Commit Impact](#group-commit-impact)
- [📊 Comparison with Competitors](#-comparison-with-competitors)
- [🧩 Features & Capabilities](#-features--capabilities)
  - [Core Storage Engine](#core-storage-engine)
  - [Durability & Journals](#durability--journals)
  - [Transactions & MVCC](#transactions--mvcc)
  - [Column Families](#column-families)
  - [APIs & Tools](#apis--tools)
- [🛡️ Durability & Crash Recovery](#️-durability--crash-recovery)
- [🕰️ How MVCC Works](#️-how-mvcc-works)
- [📚 Documentation](#-documentation)
- [🗺️ Roadmap](#️-roadmap)
- [📁 Project Structure](#-project-structure)
- [🤝 Contributing](#-contributing)
- [📄 License](#-license)
- [❓ FAQ](#-faq)
- [⭐ Support the Project](#-support-the-project)

---

## 📖 What is ScoriaDB?

ScoriaDB is an **embeddable key‑value store** written in pure Go.

It combines:

- **LSM‑tree** (MemTable, SSTable, Leveled Compaction)
- **MVCC** with Snapshot Isolation (writers never block readers)
- **ACID transactions** (interactive + WriteBatch)
- **Column Families** (independent LSM trees inside one DB)
- **WAL + Manifest** for crash recovery
- **WiscKey‑style Value Log** (efficient large values)

You can use it as a **library** (import and embed) or as a **server** (gRPC, REST, CLI, WebSocket). No cgo, no external dependencies.

---

## ✨ Why ScoriaDB?

| Feature | What you get |
|---------|--------------|
| **Embeddable** | Pure Go, no cgo, `go get` and run |
| **Ready‑to‑use server** | gRPC, REST, CLI, WebSocket — one binary |
| **ACID transactions** | Snapshot Isolation, optimistic concurrency control |
| **Column Families** | Logical data isolation with independent compaction |
| **MVCC** | Readers never block writers |
| **Cross‑language clients** | gRPC supports 13+ languages |
| **Durable by default** | WAL + fsync, Manifest, CRC32, fail‑safe VLog |
| **Fast** | Read ~140 ns, write ~750 ns (small keys) |

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
# Get JWT token
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

## 📊 Benchmarks

**Hardware:** Intel Core i3-1215U (8 threads), NVMe SSD, Go 1.23+, Linux amd64.

| Operation | Size | Time | Throughput |
|-----------|------|------|------------|
| **Put (small)** | 16 B | ~750 ns | **1.33M ops/s** |
| **Put (sync)** | 16 B | ~1,070 ns | **935K ops/s** |
| **Put (large)** | 4 KB | ~4,785 ns | **209K ops/s** |
| **Get (hit, MemTable)** | — | ~140 ns | **7.1M ops/s** |
| **Get (miss)** | — | ~310 ns | **3.2M ops/s** |
| **Scan (10k keys)** | — | ~2.2 ms | ~450 ops/s |
| **WAL (Group Commit)** | — | ~95 ns | **10.5M ops/s** |

### Group Commit Impact

| Mode | Throughput | Speedup |
|------|------------|---------|
| Sync (fsync per write) | 935K ops/s | 1× |
| **Group Commit** | **1.43M ops/s** | **1.53×** |

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
- Read performance (**7.1M ops/s**) is the **highest** among embeddable KV stores.
- Only ScoriaDB and FoundationDB offer **ACID + MVCC** in this comparison.

---

## 🧩 Features & Capabilities

### Core Storage Engine

| Component | Status |
|-----------|--------|
| MemTable (B‑tree) | ✅ |
| SSTable (block index, Bloom, prefix compression) | ✅ |
| Leveled Compaction | ✅ |
| Value Log (WiscKey, >64 bytes) | ✅ |
| Snappy / Zstd compression | ✅ |

### Durability & Journals

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

1. **WAL** — every operation is written with CRC32, `fsync` after each batch. On restart, WAL is replayed.
2. **Manifest** — JSON journal tracking all SSTable changes, `fsync` after every write.
3. **Value Log** — if corrupted, file is renamed to `.corrupt`, new file is created, data recovered from WAL.

**Recovery time:** <1 second after `kill -9`.  
**Competitors:** BadgerDB and Pebble take 9–12 seconds.

---

## 🕰️ How MVCC Works

- Every `Put` creates a new version with `commitTS` (uint64).
- Transaction `Begin()` receives `startTS` — a snapshot timestamp.
- Reads inside transaction see only versions with `commitTS ≤ startTS`.
- On `Commit()`, engine checks if any key was modified after `startTS` (`lastCommitCache` for O(1) fast path).
- Conflict → `ErrConflict` (retry required).

**Inverted timestamp trick** — keys stored as `[user_key][^commitTS]`. Newest version appears first in iteration.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → "bob" first, then "alice"
```

**Result:** Writers never block readers.

---

## 📚 Documentation

| Language | Documentation | Example |
|----------|---------------|---------|
| **Go** | [GoDoc](https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria) | `pkg/scoria` |
| **Python** | [docs/python/](docs/python/) | [example.py](docs/python/example.py) |
| **Java** | [docs/java/](docs/java/) | [example.java](docs/java/example.java) |
| **C++** | [docs/c++/](docs/c++/) | [example.cpp](docs/c++/example.cpp) |

Full documentation: [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/)

---

## 🗺️ Roadmap

| Version | Focus | Key Features | Status |
|---------|-------|--------------|--------|
| **v0.1.0** | Core stability | LSM, MVCC, ACID, CF, gRPC, CLI | ✅ |
| **v0.1.1** | CLI & docs | Interactive commands, multi‑lang docs | ✅ |
| **v0.2.0** | Write perf | Group Commit, WAL options, benchmarks | ✅ |
| **v0.3.0** | UI & TTL | Web UI, TTL, lock‑free skip list | 🚧 |
| **v0.4.0** | Performance | Zero‑copy VLog, auto GC, binary Manifest | ⏳ |
| **v1.0.0** | Distributed | Raft, sharding, distributed transactions | ⏳ |

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
Yes — gRPC examples are available in <code>docs/</code>.
</details>

<details>
<summary><b>How does ScoriaDB compare to BadgerDB?</b></summary>
<br>
ScoriaDB has <b>MVCC, Column Families, built‑in gRPC/REST</b>, and is <b>7× faster</b> on reads. BadgerDB has more mature GC.
</details>

<details>
<summary><b>Why are writes slower than reads?</b></summary>
<br>
<code>fsync</code> guarantees durability. Use WriteBatch or Group Commit (enabled by default).
</details>

<details>
<summary><b>Does zero‑copy work?</b></summary>
<br>
Not yet — planned for v0.4.0.
</details>

<details>
<summary><b>What is Group Commit and why is it enabled by default?</b></summary>
<br>
Group Commit buffers writes and performs a single <code>fsync</code> for a batch (every 10ms). It improves write throughput by 4–5× while maintaining durability. Use <code>WALOptions{GroupCommitEnabled: false}</code> to disable.
</details>

<details>
<summary><b>What are Column Families?</b></summary>
<br>
Column Families are independent LSM trees inside a single database. They provide logical isolation and allow different compaction settings per data type.
</details>

<details>
<summary><b>Does ScoriaDB support transactions across Column Families?</b></summary>
<br>
Yes. WriteBatch is atomic across multiple Column Families via a shared WAL.
</details>

<details>
<summary><b>What snapshot isolation level is used?</b></summary>
<br>
Snapshot Isolation. Readers see a consistent snapshot at <code>startTS</code>. Writers never block readers.
</details>

<details>
<summary><b>How fast is crash recovery?</b></summary>
<br>
<1 second after <code>kill -9</code>. BadgerDB and Pebble take 9–12 seconds.
</details>

<details>
<summary><b>What are the system requirements?</b></summary>
<br>
Any platform supported by Go 1.23+. ~15 MB binary, no external dependencies.
</details>

<details>
<summary><b>Can I use ScoriaDB on ARM (Raspberry Pi)?</b></summary>
<br>
Yes — pure Go, works on any architecture supported by Go (amd64, arm64, arm, etc.).
</details>

<details>
<summary><b>How can I contribute?</b></summary>
<br>
See <a href="CONTRIBUTING.md">CONTRIBUTING.md</a>. Help is especially welcome with automatic GC, lock‑free data structures, Windows/macOS testing, and Web UI.
</details>

<details>
<summary><b>Is there a Web UI?</b></summary>
<br>
Planned for v0.3.0. It will be built with Go + Alpine.js (no React/Node.js).
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
- 💻 **Submit pull requests** — every improvement matters.
- 📣 **Share the project** in your community.

---

<div align="center">
  <i>Solid as stone. Light as ash.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Full%20Documentation-blue?style=for-the-badge" alt="Documentation"></a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>