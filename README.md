<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=🔥%20Embedded%20LSM%20Database%20for%20Go%20|%20Solid%20as%20Stone%2C%20Light%20as%20Ash&fontSize=20&fontAlignY=50&animation=twinkling">
  <br><br>

  <!-- Badges -->
  <a href="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml"><img src="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/f4ga/ScoriaDB"><img src="https://goreportcard.com/badge/github.com/f4ga/ScoriaDB" alt="Go Report Card"></a>

  <!-- Language switcher (buttons) -->
  <br><br>
  <div>
    <a href="README.md"><img src="https://img.shields.io/badge/🇬🇧-English-blue?style=for-the-badge&logo=googletranslate" alt="English"></a>
    &nbsp;&nbsp;
    <a href="README_RU.md"><img src="https://img.shields.io/badge/🇷🇺-Русский-red?style=for-the-badge&logo=googletranslate" alt="Русский"></a>
  </div>

  <!-- Documentation link -->
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Full%20Documentation-blue?style=for-the-badge" alt="Documentation"></a>

  <!-- Table of contents -->
  <br>
  <table align="center" style="font-size: 1.2em; line-height: 1.8;">
    <tr>
      <td align="center">📖</td>
      <td><a href="#-what-is-scoriadb">What is ScoriaDB</a></td>
      <td align="center">👥</td>
      <td><a href="#-who-is-it-for">Who Is It For</a></td>
      <td align="center">✨</td>
      <td><a href="#-why-scoriadb">Why ScoriaDB</a></td>
    </tr>
    <tr>
      <td align="center">📊</td>
      <td><a href="#-benchmarks">Benchmarks</a></td>
      <td align="center">📊</td>
      <td><a href="#-comparison-with-redis">Comparison with Redis</a></td>
      <td align="center">🧩</td>
      <td><a href="#-features--capabilities">Features & Capabilities</a></td>
    </tr>
    <tr>
      <td align="center">🛡️</td>
      <td><a href="#-durability-and-crash-recovery">Durability & Crash Recovery</a></td>
      <td align="center">🕰️</td>
      <td><a href="#-how-mvcc-works">How MVCC Works</a></td>
      <td align="center">📚</td>
      <td><a href="#-documentation">Documentation</a></td>
    </tr>
    <tr>
      <td align="center">📈</td>
      <td><a href="#-v010-release-criteria">v0.1.0 Release Criteria</a></td>
      <td align="center">📁</td>
      <td><a href="#-project-structure">Project Structure</a></td>
      <td align="center">🗺️</td>
      <td><a href="#-version-roadmap">Version Roadmap</a></td>
    </tr>
    <tr>
      <td align="center">📄</td>
      <td><a href="#-license">License</a></td>
      <td align="center">❓</td>
      <td><a href="#-faq">FAQ</a></td>
      <td align="center">🤝</td>
      <td><a href="#-support-the-project">Support the Project</a></td>
    </tr>
  </table>
</div>

<br>

## 📖 What is ScoriaDB?

**ScoriaDB** is an embeddable key‑value database written in pure Go.  
Under the hood — an LSM tree, MVCC, Column Families, WAL, and a WiscKey‑style Value Log.

- **As a library** – import `github.com/f4ga/scoriadb/pkg/scoria` and get an LSM engine inside your own process. No external dependencies, no cgo.
- **As a server** – run a single binary and it serves gRPC, REST, WebSocket, and CLI. A ready‑made backend for microservices in any language.

> The project is on the home stretch to **v0.1.0**. Almost everything works, tests are green.

---

## 👥 Who is it for?

| User type | Use case |
|:---|:---|
| **Go developer** | Embed a KV store into your service, CLI tool, agent, or daemon. No separate database needed. |
| **IoT / Edge engineer** | A resource‑constrained device needs local storage with remote access via gRPC or REST. |
| **Microservice team** | One ScoriaDB server, clients in Python, Java, C++, Rust, Node.js, C# – via gRPC. |
| **Log analyst** | Embed a database to index and search logs (demo tool **Scorix** already works). |
| **Student / hobbyist** | Learn LSM, MVCC, compaction from open, readable source code. |

---

## ✨ Why ScoriaDB?

| Advantage | What it gives you |
| :--- | :--- |
| **Embeddable** | Pure Go. No `apt-get install rocksdb`, no cgo. |
| **Ready‑made server** | gRPC, REST, CLI, WebSocket – out of the box. Just run `scoria-server`. |
| **ACID transactions** | Snapshot Isolation, interactive transactions, atomic WriteBatch. |
| **Column Families** | Independent LSM trees – tune compaction per data type. |
| **MVCC** | Readers never block writers (concurrency isn't perfect yet, but it works). |
| **Cross‑language access** | gRPC clients for 12+ languages (Python, Java, C++, …). |
| **Reliability** | WAL + Manifest with fsync, CRC32 on every block. Data survives `kill -9`. |
| **Performance** | ~150 ns reads, ~1 µs writes (small keys). |

---

## 📊 Benchmarks

*Test machine: Intel Core i3-1215U (8 threads), 16GB RAM, NVMe SSD, Go 1.23+, Linux amd64.*  
*Run with: `go test -bench=. -count=5 ./internal/engine ./pkg/scoria | benchstat`*

| Operation | Value size | Time (ns/op) | Throughput |
|-----------|------------|--------------|-------------|
| `engine.Put` (small) | 16 bytes | **1,070** | ~935,000 ops/s |
| `engine.Put` (large) | 4 KB (Value Log) | **4,785** | ~209,000 ops/s |
| `engine.Get` (hit) | key in MemTable | **152** | ~6,580,000 ops/s |
| `engine.Get` (miss) | key not exists | **310** | ~3,225,000 ops/s |
| `ScoriaDB.Put` (public API) | 16 bytes | **1,063** | ~940,000 ops/s |
| `ScoriaDB.Get` (public API) | 16 bytes | **144** | ~6,940,000 ops/s |

> **Notes:** Batch writes amortize fsync overhead – tens of microseconds per write on NVMe. Reads never stall even under heavy writes – MVCC works.

---

## 📊 Comparison with Redis

**Important:** ScoriaDB is **not** a Redis replacement. Redis is an in‑memory cache; ScoriaDB is an embedded disk‑based store with transactions.

| Feature | ScoriaDB (embedded) | Redis CE (network) |
|:---|:---|:---|
| **Deployment** | Library or server | Only server |
| **Network overhead** | None (embedded mode) | TCP (0.1–0.2 ms) |
| **Read latency** | ~150 ns | ~0.24–0.31 ms |
| **Write latency** | ~1,070 ns | ~0.45 ms |
| **Persistence** | Full, with fsync | Optional (RDB/AOF) |
| **Transactions** | ACID, Snapshot Isolation | None (pipelining) |
| **MVCC** | Yes | No |
| **Column Families** | Yes | No |
| **Embeddable** | Yes, `import` | No |

**Bottom line:** Need a fast cache? Use Redis. Need a reliable embeddable database with remote access? Try ScoriaDB.

---

## 🧩 Features & Capabilities

### LSM Engine

| Component | Status |
|:---|:---:|
| MemTable (B‑tree) | ✅ |
| SSTable (block index, Bloom, prefix compression) | ✅ |
| Leveled Compaction | ✅ |
| Value Log (WiscKey, >64 bytes) | ✅ |
| Compression (Snappy, Zstd) | ✅ |

### Durability & Journals

| Component | Status |
|:---|:---:|
| WAL + fsync + recovery | ✅ |
| Manifest + fsync | ✅ |
| Block CRC32 | ✅ |
| Fail‑safe VLog | ✅ |

### Transactions & MVCC

| Feature | Status |
|:---|:---:|
| MVCC, Snapshot Isolation | ✅ |
| Interactive transactions | ✅ |
| WriteBatch | ✅ |
| Conflict detection | ✅ |

### Column Families

| Feature | Status |
|:---|:---:|
| Independent LSM trees | ✅ |
| Shared WAL (atomic cross‑CF) | ✅ |

### APIs

| Interface | Status |
|:---|:---:|
| Embedded Go API | ✅ |
| gRPC | ✅ |
| REST | ✅ |
| WebSocket | ✅ |
| CLI (Cobra, interactive) | ✅ |

### Security & Monitoring

| Feature | Status |
|:---|:---:|
| JWT authentication | ✅ |
| Roles (admin, readwrite, readonly) | ✅ |
| Seed user (`admin/admin`) | ✅ |
| Prometheus metrics | ✅ |
| Health / ready endpoints | ✅ |

---

## 🛡️ Durability and Crash Recovery

ScoriaDB is designed to **not lose data** even after sudden power loss:

1. **WAL** – every operation is written with CRC32 and `fsync` before entering MemTable. On crash, WAL is replayed.
2. **Manifest** – metadata journal with `fsync`. On startup, exact file set is restored.
3. **Value Log** – on magic mismatch, file is renamed to `.corrupt`, a new one is created, and data is recovered from WAL.

**Price:** `fsync` on every WriteBatch is ~5× slower than buffered writes. Group Commit coming in v0.2.0.

---

## 🕰️ How MVCC Works

- Every `Put` creates a new version with a `commitTS` timestamp.
- Transaction `Begin()` gets a `startTS` snapshot.
- Reads see versions with `commitTS ≤ startTS`.
- On `Commit()`: if any modified key has a newer version (`commitTS > startTS`) → `ErrConflict`. Retry the transaction.
- **Writers never block readers.**

**Inverted timestamp:** store `^commitTS` so fresh records appear first in iterator.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Iterator shows bob first, then alice.
```

---

## 📚 Documentation

Complete documentation is available in the [`docs/`](docs/) directory:

| Language | Documentation | Example |
|:---|:---|:---|
| **Go (embedded)** | [Go-Embedded](docs/README.md#go-embedded-api) | `pkg/scoria` |
| **Python** | [python-doc.md](docs/python/python-doc.md) | [example.py](docs/python/example.py) |
| **Java** | [java-doc.md](docs/java/java-doc.md) | [example.java](docs/java/example.java) |
| **C++** | [cpp-doc.md](docs/c++/cpp-doc.md) | [example.cpp](docs/c++/example.cpp) |

**Quick start with Docker:**

```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
docker compose -f deployments/docker-compose.yml up --build
docker exec -it scoria-server ./scoria-cli admin auth admin admin
docker exec -it scoria-server ./scoria-cli --token <token> set hello world
```

**Or build locally:**

```bash
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli
./scoria-server &
TOKEN=$(./scoria-cli admin auth admin admin)
./scoria-cli --token "$TOKEN" set hello world
```

**For Go embedded API:**

```go
import "github.com/f4ga/ScoriaDB/pkg/scoria"
db, _ := scoria.NewScoriaDB("./data")
defer db.Close()
db.Put([]byte("hello"), []byte("world"))
```

---

## 📈 v0.1.0 Release Criteria

All items ✅ :

| Category | Status |
|:---|:---:|
| LSM engine, SSTable, Compaction | ✅ |
| MVCC, Snapshot Isolation | ✅ |
| Transactions, WriteBatch | ✅ |
| Column Families | ✅ |
| Embedded Go, gRPC, REST, CLI | ✅ |
| JWT, roles | ✅ |
| Docker, CI/CD | ✅ |
| Prometheus metrics, health | ✅ |
| Unit, integration, stress tests | ✅ |

---

## 📁 Project Structure

```
scoriadb/
├── cmd/                     # Entry points (server, CLI)
├── internal/                # Core (engine, MVCC, txn, CF, API)
├── pkg/scoria/              # Public embedded API
├── proto/                   # Protobuf specs
├── tests/                   # E2E and stress tests
├── deployments/             # Docker
└── docs/                    # Documentation (Python, Java, C++ guides)
```

---

## 🗺️ Version Roadmap

| Version | What will be included |
|:---|:---|
| **v0.1.0** | ✅ Stable core, gRPC, CLI, Docker, tests |
| **v0.2.0** | Web UI, Group Commit, TTL, binary Manifest, VFS fix |
| **v0.3.0** | Lock‑free skip list, true zero‑copy, automatic GC |
| **v1.0.0** | Raft replication, sharding, distributed transactions, Sorted Sets |

---

## 📄 License

**Apache License 2.0** – see [LICENSE](LICENSE).

Permitted: use, modify, distribute, sublicense, commercial use.  
Not permitted: use the name ScoriaDB.

---

## ❓ FAQ

**Is ScoriaDB production‑ready?**  
v0.1.0 – for moderate workloads (10–20 concurrent writes). For 1000+ goroutines, wait for v0.3.0 (lock‑free MemTable, automatic GC).

**Does zero‑copy actually work?**  
No. Current implementation copies from mmap for safety. True zero‑copy in v0.3.0.

**Why are writes slow?**  
`fsync` on every batch. Group Commit in v0.2.0 will reduce latency 3–5×.

**Can I use it from Python?**  
Yes, via gRPC – see [docs/python/](docs/python/).

**Can I contribute?**  
Yes! Help is needed with automatic GC, lock‑free structures, Windows/macOS testing, and Web UI.

---

## 🤝 Support the Project

- ⭐ **Star** the repository
- 🐛 **Report bugs** via Issues
- 💻 **Submit PRs**
- 📣 **Share the project**

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