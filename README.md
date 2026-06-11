<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=🔥%20Embedded%20LSM%20Database%20for%20Go%20|%20Solid%20as%20Stone%2C%20Light%20as%20Ash&fontSize=20&fontAlignY=50&animation=twinkling">
  <br><br>

  <!-- Badges -->
  <a href="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml"><img src="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>

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
      <td align="center">👥</td><td><a href="#-who-is-it-for">Who is it for</a></td>
      <td align="center">✨</td><td><a href="#-why-scoriadb">Why ScoriaDB</a></td>
    </tr>
    <tr><td align="center">📊</td><td><a href="#-benchmarks">Benchmarks</a></td>
      <td align="center">📊</td><td><a href="#-comparison-with-redis">Comparison with Redis</a></td>
      <td align="center">🧩</td><td><a href="#-features--capabilities">Features &amp; Capabilities</a></td>
    </tr>
    <tr><td align="center">🛡️</td><td><a href="#-durability-and-crash-recovery">Durability &amp; Crash Recovery</a></td>
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
It combines **LSM‑tree performance**, **MVCC with ACID transactions**, **Column Families**, **WAL + Manifest for crash recovery**, and **WiscKey‑style Value Log** — all in a single binary with no external dependencies.

- **As a library** – `import "github.com/f4ga/ScoriaDB/pkg/scoria"`. You get a ready‑to‑use LSM engine inside your Go process. No cgo, no external services.
- **As a server** – run `scoria-server` and it immediately speaks gRPC (accessible from **13 languages**), REST, WebSocket, and a feature‑rich administrative CLI.

**What makes ScoriaDB unique**  
- **Pure Go without cgo** – easy to build, cross‑platform, fully debuggable.  
- **First Go‑native LSM with MVCC + Snapshot Isolation** – writers never block readers.  
- **Column Families** – independent LSM trees inside one database, atomic writes across CFs.  
- **Group Commit in WAL** – 4–5x faster durable writes without sacrificing reliability.  
- **Ready‑to‑use server** – gRPC, REST, CLI, WebSocket (Web UI planned).  
- **Multi‑language clients** – ready‑made gRPC examples for Python, Java, C++.

> Current stable version: **v0.2.0** (Group Commit released). All core components are tested and documented.

---

## 👥 Who is it for?

| User type | Use case |
|:---|:---|
| **Go developer** | Embed a fast KV store into your service, CLI, or agent – no separate database process needed. |
| **IoT / Edge engineer** | Local storage with remote access via gRPC/REST on resource‑constrained devices. |
| **Microservice team** | One server, clients in many languages (gRPC). |
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
| **Cross‑language access** | gRPC clients for 13 languages. |
| **Reliability** | WAL + Manifest with fsync, CRC32, fail‑safe VLog. |
| **Performance** | Read ~150 ns, write ~1 µs (small keys). |

---

## 📊 Benchmarks

**Test environment:** Intel Core i3-1215U (8 threads), NVMe SSD, Go 1.23+, Linux amd64.  
**Command:** `go test -bench=. -count=5 ./internal/engine ./pkg/scoria | benchstat`

| Operation | Value size | Time (ns/op) | Throughput (ops/s) |
|----------|------------|--------------|-------------------|
| Put (small) | 16 B | **1 070** | ~935 000 |
| Put (large, VLog) | 4 KB | **4 785** | ~209 000 |
| Get (hit, MemTable) | – | **152** | ~6 580 000 |
| Get (miss) | – | **310** | ~3 225 000 |
| **Group Commit WAL (sequential)** | ~50 B | **94.9** | ~10 540 000 |

> **Batch write** (WriteBatch of 100 operations) delivers **~970 000 ops/s** with full durability – fsync overhead is amortized.  
> **Reads never stall** – even under heavy concurrent writes (MVCC).

### Group Commit Impact on WAL

| Mode | Latency (ns/op) | Throughput (ops/s) |
|:---|:---:|:---:|
| Sync (fsync per write) | 454 | 2 200 000 |
| Group Commit (10 ms) | 94.9 | 10 500 000 |

<<<<<<< HEAD
*Group Commit is released and enabled by default in server mode.*
=======
*Group Commit is **enabled by default** since v0.2.0. Use `WALOptions{GroupCommitEnabled: false}` or `export WAL_GROUP_COMMIT=false` to disable.*
>>>>>>> a74f379 (feat(wal): enable Group Commit by default, remove dead code)

---

## 📊 Comparison with Redis

ScoriaDB is **not** a Redis replacement – different niches. Redis: in‑memory cache. ScoriaDB: disk‑based, durable, embeddable KV.

| Feature | ScoriaDB (embedded) | Redis CE (network) |
|:---|:---|:---|
| Deployment | Library or server | Separate server |
| Network overhead | none | ~0.1–0.2 ms TCP |
| Read latency | **~150 ns** | ~0.24–0.31 ms |
| Write latency (sync) | **~1 070 ns** | ~0.45 ms (AOF everysec) |
| Persistence | **full fsync** | optional (RDB/AOF) |
| Transactions | **ACID + Snapshot Isolation** | none (pipelining only) |
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
| Embeddable Go API (`DB`, `CFDB`) | ✅ |
| gRPC (streaming Scan, transactions) | ✅ |
| REST + WebSocket | ✅ |
| CLI (`scoria`) with interactive shell | ✅ |
| JWT authentication (admin/readwrite/readonly roles) | ✅ |
| Prometheus metrics, health/ready endpoints | ✅ |
| Docker & docker‑compose | ✅ |

---

## 🛡️ Durability and Crash Recovery

1. **WAL** – every operation is appended with CRC32, `fsync` is called after each batch (or after group flush). On restart, the WAL is replayed.
2. **Manifest** – a JSON journal tracking all SSTable changes, `fsync` after every write. On startup, it reconstructs the exact file set.
3. **Value Log** – if the magic number is corrupted, the file is renamed to `.corrupt`, a new one is created, and data is recovered from the WAL.

**Price** – `fsync` slows writes by ~5x, but **Group Commit** fully amortizes the cost, providing a 5x speedup. For high write loads, use WriteBatch.

---

## 🕰️ How MVCC Works

- Every `Put` creates a new version with a `commitTS` (uint64).
- A transaction calls `Begin()` and receives `startTS` – a snapshot timestamp.
- Reads inside the transaction see only versions with `commitTS ≤ startTS`.
- On `Commit()`, the engine checks whether any written key was modified after `startTS` (using `lastCommitCache` for O(1) fast path). If a conflict is found → `ErrConflict`, the transaction must be retried.

**Inverted timestamp trick** – keys are stored as `[user_key][^commitTS]`. Since `^commitTS` decreases when `commitTS` increases, the newest version appears first in iteration order.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → shows "bob" first, then "alice"
```

**Result:** writers never block readers. Snapshot Isolation is guaranteed.

---

## 📚 Documentation

Full documentation is in the [`docs/`](docs/) folder and also available at [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/).

| Language | Documentation | Example |
|:---|:---|:---|
| **Go (API)** | [GoDoc](https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria) | `pkg/scoria` |
| **Go (API)** | [GitHub Pages](https://f4ga.github.io/ScoriaDB/#go-embedded-api) | |
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

This release focuses on **write performance**, durability management, and documentation.

| Feature / Improvement | Description |
|:---|:---|
| **Group Commit in WAL (enabled by default)** | Buffered writes with periodic fsync (10 ms interval). Improves write throughput by 4–5× without sacrificing durability. **Enabled by default** — use `WALOptions{GroupCommitEnabled: false}` to disable. |
| **WAL group commit writer** | Asynchronous flush loop + ticker, configurable interval. |
| **Public API for WAL options** | `OpenWALWithOptions`, `EngineOptions`, `DefaultWALOptions()` with Group Commit enabled. |
>>>>>>> a74f379 (feat(wal): enable Group Commit by default, remove dead code)
| **Multi‑language documentation** | Full gRPC examples and guides for **Python, Java, C++** (see `docs/`). |
| **Extended benchmark suite** | Sync vs Group Commit comparison, different value sizes. |
| **Crash recovery tests** | Durability verified with Group Commit enabled. |

> All core features from v0.1.0 remain (LSM, MVCC, transactions, Column Families, gRPC/REST/CLI, etc.). v0.2.0 is backward compatible.

---

## 📁 Project Structure

```
scoriadb/
├── cmd/                     # server and CLI entry points
├── internal/                # engine, mvcc, txn, cf, api
├── pkg/scoria/              # public embeddable API
├── proto/                   # gRPC protobuf definitions
├── tests/                   # integration and stress tests
├── deployments/             # Docker files
└── docs/                    # multi‑language documentation
```

---

## 🗺️ Version Roadmap

| Version | Focus | Key Features | Target Release |
|:---|:---|:---|:---|
| **v0.1.0** | First stable | LSM, MVCC, ACID, Column Families, gRPC, CLI, basic GC | April 2026 ✅ |
| **v0.1.1** | CLI & documentation | Interactive commands (`create-cf`, `list-cf`, `whoami`, `stats`, history, export), Python/Java/C++ docs | May 2026 ✅ |
| **v0.2.0** | Write performance | Group Commit (WAL), WAL options, crash recovery tests | May 2026 ✅ |
| **v0.3.0** | Web UI & TTL | React dashboard, TTL (time‑to‑live) for records | June 2026 |
| **v0.4.0** | Core rewrite | Lock‑free skip list (replace B‑tree), true zero‑copy Value Log, automatic incremental GC | Q4 2026 |
| **v1.0.0** | Distributed mode | Raft replication, range sharding, distributed ACID transactions (2PC), native data structures (Sorted Sets, Lists, JSON indexes) | 2027 |

> **Note:** Versions marked ✅ are already released. The roadmap may change based on feedback and contributor activity.

---

## 📄 License

**Apache License 2.0** – see the [LICENSE](LICENSE) file.  
You may use, modify, distribute, and sublicense. The name ScoriaDB may not be used to endorse derived products without permission.

---

## ❓ FAQ

**Is ScoriaDB production‑ready?**  
v0.2.0 is stable and tested under load. For 1000+ concurrent writers, wait for lock‑free skip list (v0.4.0).

**Why are writes slower than reads?**  
`fsync` guarantees durability. Use WriteBatch or enable Group Commit (enabled by default).

**Can I use it from Python / Java / C++?**  
Yes – see [`docs/`](docs/) for full gRPC examples.

**How does ScoriaDB compare to BadgerDB?**  
ScoriaDB offers **MVCC, Column Families, interactive transactions**, built‑in gRPC/REST, and significantly **faster reads** (6.6M ops/s vs ~400k). BadgerDB has a more mature Value Log GC, but ScoriaDB with Group Commit provides better synchronous write throughput.

**Does zero‑copy work?**  
Not yet – the current implementation copies from mmap to avoid SIGSEGV. True zero‑copy is planned for v0.4.0.

**How can I contribute?**  
See [CONTRIBUTING.md](CONTRIBUTING.md). Help is especially welcome with automatic GC, lock‑free data structures, Windows/macOS testing, and Web UI.

---

## 🤝 Support the Project

- ⭐ **Star** the repository on GitHub.
- 🐛 **Report bugs** via Issues.
- 💻 **Submit pull requests** – every improvement matters.
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
