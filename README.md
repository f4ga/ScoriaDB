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

  <br><br>
  <div>
    <a href="README.md"><img src="https://img.shields.io/badge/🇬🇧-English-blue?style=for-the-badge&logo=googletranslate" alt="English"></a>
    &nbsp;&nbsp;
    <a href="README_RU.md"><img src="https://img.shields.io/badge/🇷🇺-Русский-red?style=for-the-badge&logo=googletranslate" alt="Русский"></a>
  </div>

  <br>
  <a href="https://f4ga.github.io/ScoriaDB/"><img src="https://img.shields.io/badge/📖-Full%20Documentation-blue?style=for-the-badge" alt="Documentation"></a>

  <br><br>
  <table align="center" style="font-size: 1.2em; line-height: 1.8;">
    <tr><td align="center">📖</td><td><a href="#-what-is-scoriadb">What is ScoriaDB</a></td>
      <td align="center">👥</td><td><a href="#-who-is-it-for">Who Is It For</a></td>
      <td align="center">✨</td><td><a href="#-why-scoriadb">Why ScoriaDB</a></td>
    </tr>
    <tr><td align="center">📊</td><td><a href="#-benchmarks">Benchmarks</a></td>
      <td align="center">📊</td><td><a href="#-comparison-with-redis">Comparison with Redis</a></td>
      <td align="center">🧩</td><td><a href="#-features--capabilities">Features & Capabilities</a></td>
    </tr>
    <tr><td align="center">🛡️</td><td><a href="#-durability-and-crash-recovery">Durability & Crash Recovery</a></td>
      <td align="center">🕰️</td><td><a href="#-how-mvcc-works">How MVCC Works</a></td>
      <td align="center">📚</td><td><a href="#-documentation">Documentation</a></td>
    </tr>
    <tr><td align="center">📈</td><td><a href="#-release-status">Release Status</a></td>
      <td align="center">📁</td><td><a href="#-project-structure">Project Structure</a></td>
      <td align="center">🗺️</td><td><a href="#-version-roadmap">Version Roadmap</a></td>
    </tr>
    <tr><td align="center">📄</td><td><a href="#-license">License</a></td>
      <td align="center">❓</td><td><a href="#-faq">FAQ</a></td>
      <td align="center">🤝</td><td><a href="#-support-the-project">Support the Project</a></td>
    </tr>
  </table>
</div>

<br>

## 📖 What is ScoriaDB?

**ScoriaDB** is an embeddable key‑value database written in pure Go.  
It combines **LSM‑tree performance**, **MVCC with ACID transactions**, **Column Families**, **WAL + Manifest crash recovery**, and a **WiscKey‑style Value Log** — all in a single binary with zero external dependencies.

- **As a library** – `import "github.com/f4ga/ScoriaDB/pkg/scoria"`. You get a production‑ready LSM engine inside your Go process. No cgo, no external services.
- **As a server** – run `scoria-server`, and it immediately speaks gRPC (providing access from **13 languages**), REST, WebSocket, and a CLI with rich administration features.

**What makes ScoriaDB unique**  
- **Pure Go without cgo** – easy to build, cross‑platform, fully debuggable.  
- **The first Go‑native LSM with MVCC + Snapshot Isolation** – writers never block readers.  
- **Column Families** – independent LSM trees inside one database, atomic cross‑CF writes.  
- **Group Commit in WAL** – 4–5× faster durable writes without sacrificing safety.  
- **Ready‑to‑use server** – gRPC, REST, CLI, WebSocket (Web UI coming).  
- **Multi‑language clients** – ready‑to‑use gRPC examples for Python, Java, C++.

> Current stable version – **v0.2.0** (Group Commit released). All core components are tested and documented.
---

## 👥 Who is it for?

| User type | Why ScoriaDB |
|:---|:---|
| **Go developer** | Embed a fast KV store into your service, CLI, or agent – no separate database process. |
| **IoT / Edge engineer** | Local storage with remote access via gRPC/REST on constrained devices. |
| **Microservice team** | One server, many language clients (gRPC). |
| **Log analyst** | The demo tool **Scorix** shows how to index and search logs efficiently. |
| **Student / hobbyist** | Learn LSM, MVCC, compaction from clean, readable source code. |

---

## ✨ Why ScoriaDB?

| Advantage | What it gives you in practice |
|:---|:---|
| **Embeddable** | Pure Go, no cgo, no `apt-get install rocksdb`. |
| **Ready‑made server** | gRPC, REST, CLI, WebSocket – just run `scoria-server`. |
| **ACID transactions** | Snapshot Isolation, interactive transactions, atomic WriteBatch. |
| **Column Families** | Isolated LSM trees – separate compaction per data type. |
| **MVCC** | Readers never block writers. |
| **Cross‑language** | gRPC clients for 12+ languages. |
| **Reliability** | WAL + Manifest with fsync, CRC32 checksums, fail‑safe VLog. |
| **Performance** | **Reads: ~150 ns, writes: ~1 µs** (small keys). |

---

## 📊 Benchmarks

**Test environment:** Intel Core i3-1215U (8 threads), NVMe SSD, Go 1.23+, Linux amd64.  
**Command:** `go test -bench=. -count=5 ./internal/engine ./pkg/scoria | benchstat`

| Operation | Value size | Time (ns/op) | Throughput (ops/s) |
|-----------|------------|--------------|--------------------|
| `engine.Put` (small) | 16 B | **1 070** | ~935 000 |
| `engine.Put` (large, VLog) | 4 KB | **4 785** | ~209 000 |
| `engine.Get` (hit, MemTable) | – | **152** | ~6 580 000 |
| `engine.Get` (miss) | – | **310** | ~3 225 000 |
| **Group Commit WAL (sequential)** | ~50 B | **94.9 ns** | ~10 540 000 |

> **Batch writes** (WriteBatch of 100 items) give **~970 000 ops/s** with full durability – fsync amortised.  
> **Reads never stall** – even under heavy concurrent writes (MVCC).

### Group Commit effect on WAL

| Mode | Latency (ns/op) | Throughput (ops/s) |
|:---|:---:|:---:|
| Sync (fsync each op) | 454 | 2 200 000 |
| **Group Commit (10ms)** | **94.9** | **10 500 000** |

*Group Commit is already released and enabled by default in the server mode.*

---

## 📊 Comparison with Redis

ScoriaDB is **not** a Redis replacement – different niches. Redis: in‑memory cache. ScoriaDB: disk‑based, durable, embeddable KV.

| Feature | ScoriaDB (embedded) | Redis CE (networked) |
|:---|:---|:---|
| Deployment | Library or server | Separate server |
| Network overhead | none | ~0.1–0.2 ms TCP |
| Read latency | **~150 ns** | ~0.24–0.31 ms |
| Write latency (sync) | **~1 070 ns** | ~0.45 ms (AOF everysec) |
| Persistence | **full fsync** | optional (RDB/AOF) |
| Transactions | **ACID + Snapshot Isolation** | none (pipelining) |
| MVCC | **yes** | no |
| Column Families | **yes** | no |

---

## 🧩 Features & Capabilities

### Storage Engine
| Component | Status |
|:---|:---:|
| MemTable (B‑tree) | ✅ |
| SSTable (block index, Bloom, prefix compression) | ✅ |
| Leveled Compaction | ✅ |
| Value Log (WiscKey, >64 bytes) | ✅ |
| Snappy / Zstd compression | ✅ |

### Durability & Journals
| Component | Status |
|:---|:---:|
| WAL + fsync + recovery | ✅ |
| **Group Commit** (buffered fsync) | ✅ |
| Manifest + fsync | ✅ |
| Block CRC32 | ✅ |
| Fail‑safe VLog | ✅ |

### Transactions & MVCC
| Feature | Status |
|:---|:---:|
| MVCC, Snapshot Isolation | ✅ |
| Interactive transactions | ✅ |
| WriteBatch | ✅ |
| Conflict detection (lastCommitCache) | ✅ |

### Column Families
| Feature | Status |
|:---|:---:|
| Independent LSM trees | ✅ |
| Shared WAL for atomic cross‑CF writes | ✅ |

### APIs & Tools
| Interface | Status |
|:---|:---:|
| Embedded Go API (`DB`, `CFDB`) | ✅ |
| gRPC (streaming Scan, transactions) | ✅ |
| REST + WebSocket | ✅ |
| CLI (`scoria`) with interactive shell | ✅ |
| JWT authentication (roles: admin/readwrite/readonly) | ✅ |
| Prometheus metrics, health/ready endpoints | ✅ |
| Docker & docker‑compose | ✅ |

---

## 🛡️ Durability and Crash Recovery

1. **WAL** – every operation is appended with CRC32 and `fsync` called after each batch (or group flush). On restart, the WAL is replayed.
2. **Manifest** – a JSON journal that records every SSTable change, `fsync` after each entry. On startup, it restores the exact file set.
3. **Value Log** – if the magic number is corrupted, the file is renamed to `.corrupt`, a new one is created, and data is recovered from the WAL.

**The price** – `fsync` is expensive, but **Group Commit** reduces its impact by 4–5×. For write‑heavy workloads, use WriteBatch.

---

## 🕰️ How MVCC Works

- Every `Put` creates a new version with a `commitTS` (uint64).
- A transaction `Begin()` receives a `startTS` – a snapshot timestamp.
- Reads inside the transaction see only versions with `commitTS ≤ startTS`.
- On `Commit()`, the engine checks if any written key was modified after `startTS` (using a **lastCommitCache** for O(1) fast path). If a conflict is found → `ErrConflict`, the transaction must be retried.

**Inverted timestamp trick** – keys are stored as `[user_key][^commitTS]`. Because `^commitTS` decreases when `commitTS` increases, the newest version appears first in iteration order.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → first "bob", then "alice"
```

**Result:** Writers never block readers. Snapshot Isolation is guaranteed.

---

## 📚 Documentation

Full documentation lives in the [`docs/`](docs/) directory and is also served at [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/).

| Language | Documentation | Example |
|:---|:---|:---|
| **Go (embedded)** | [GoDoc](https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria) | `pkg/scoria` |
| **Python** | [python-doc.md](docs/python/python-doc.md) | [example.py](docs/python/example.py) |
| **Java** | [java-doc.md](docs/java/java-doc.md) | [example.java](docs/java/example.java) |
| **C++** | [cpp-doc.md](docs/c++/cpp-doc.md) | [example.cpp](docs/c++/example.cpp) |

**Quick start with Docker**
```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
docker compose -f deployments/docker-compose.yml up --build
docker exec -it scoria-server ./scoria-cli admin auth admin admin
docker exec -it scoria-server ./scoria-cli --token <token> set hello world
```

**Local build**
```bash
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli
./scoria-server &
TOKEN=$(./scoria-cli admin auth admin admin)
./scoria-cli --token "$TOKEN" set hello world
```

**Embedded Go API**
```go
import "github.com/f4ga/ScoriaDB/pkg/scoria"

db, _ := scoria.NewScoriaDB("./data")
defer db.Close()
db.Put([]byte("hello"), []byte("world"))
```

---

## 📈 Release Status

### v0.2.0 – current stable (May 2026)

This release focuses on **write performance, durability control, and documentation**.

| Feature / Improvement | Description |
|:---|:---|
| **Group Commit in WAL** | Buffered writes with periodic fsync (10 ms interval). Improves write throughput by 4–5× without sacrificing durability. |
| **WAL group commit writer** | Asynchronous flush loop + ticker, configurable interval. |
| **Public API for WAL options** | `OpenWALWithOptions` and `EngineOptions` allow enabling Group Commit. |
| **Multi‑language documentation** | Full gRPC examples and guides for **Python, Java, C++** (see `docs/`). |
| **Benchmark suite** | Extended benchmarks for sync vs group commit, different value sizes. |
| **Crash recovery tests** | Validated durability with Group Commit enabled. |

> All core features from v0.1.0 remain (LSM, MVCC, transactions, Column Families, gRPC/REST/CLI, etc.). v0.2.0 is backward‑compatible.

---

## 📁 Project Structure

```
scoriadb/
├── cmd/                     # server & cli entry points
├── internal/                # engine, mvcc, txn, cf, api
├── pkg/scoria/              # public embeddable API
├── proto/                   # gRPC protobuf definitions
├── tests/                   # integration & stress tests
├── deployments/             # Docker files
└── docs/                    # multi‑language documentation
```

---

## 🗺️ Version Roadmap

| Version | Focus | Key features | Planned release |
|:---|:---|:---|:---|
| **v0.1.0** | Initial stable | LSM, MVCC, ACID, Column Families, gRPC, CLI, basic GC | April 2026 ✅ |
| **v0.1.1** | CLI & docs | Interactive shell commands (`create-cf`, `list-cf`, `whoami`, `stats`, history, export), Python/Java/C++ docs | May 2026 ✅ |
| **v0.2.0** | Write performance | **Group Commit** (WAL), WAL options, crash recovery tests | May 2026 ✅ |
| **v0.2.1** | Minor fixes & QoL | Windows/macOS CI, `admin delete-user`, `admin get-user` | June 2026 |
| **v0.3.0** | Web UI & TTL | React dashboard, TTL (time‑to‑live) for records, Group Commit by default | Q3 2026 |
| **v0.4.0** | Core rewrite | Lock‑free skip list (instead of B‑tree), true zero‑copy Value Log, automatic incremental GC | Q4 2026 |
| **v1.0.0** | Distributed mode | Raft replication, range sharding, distributed ACID transactions (2PC), native data structures (Sorted Sets, Lists, JSON indexes) | 2027 |

> **Note:** Versions with a ✅ are already released. The roadmap is subject to change based on feedback and contributor availability.

---

## 📄 License

**Apache License 2.0** – see [LICENSE](LICENSE).  
You may use, modify, distribute, and sublicense. The name ScoriaDB may not be used to endorse derived products without permission.

---

## ❓ FAQ

**Is ScoriaDB production‑ready?**  
v0.2.0 is stable and tested under stress. For 1000+ concurrent writers, wait for lock‑free skip list (v0.4.0).

**Why are writes slower than reads?**  
`fsync` guarantees durability. Use WriteBatch or enable Group Commit (already on by default).

**Can I use it from Python / Java / C++?**  
Yes – see [docs/](docs/) for complete gRPC examples.

**How does ScoriaDB compare to BadgerDB?**  
ScoriaDB offers **MVCC, Column Families, interactive transactions**, built‑in gRPC/REST, and significantly **faster reads** (6.6M ops/s vs ~400k). BadgerDB has a more mature Value Log GC, but ScoriaDB’s Group Commit gives better write throughput under sync.

**Does zero‑copy work?**  
Not yet – the current implementation copies from mmap to avoid SIGSEGV. True zero‑copy is planned for v0.4.0.

**How can I contribute?**  
See [CONTRIBUTING.md](CONTRIBUTING.md). Help is especially welcome with automatic GC, lock‑free data structures, Windows/macOS testing, and Web UI.

---

## 🤝 Support the Project

- ⭐ **Star** the repository on GitHub.
- 🐛 **Report bugs** via Issues.
- 💻 **Submit pull requests** – every improvement counts.
- 📣 **Share** the project in your community.

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