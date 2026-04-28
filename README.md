<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn" alt="Header">
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&section=header&text=🔥%20Embedded%20LSM%20Database%20for%20Go%20|%20Solid%20as%20Stone%2C%20Light%20as%20Ash&fontSize=20&fontAlignY=50&animation=twinkling" alt="Subtitle">
  <br><br>

  [![CI](https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg)](https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml)
  [![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev/)
  [![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

  <div align="center">

  [🇬🇧 English](README.md) &nbsp;&nbsp;|&nbsp;&nbsp; [🇷🇺 Русский](README_RU.md)

  </div>

  <br>
  <table align="center" style="font-size: 1.4em; line-height: 2;">
    <tr><td>📖</td><td><a href="#-what-is-scoriadb">What is ScoriaDB</a></td></tr>
    <tr><td>👥</td><td><a href="#-who-is-it-for">Who is it for</a></td></tr>
    <tr><td>✨</td><td><a href="#-why-scoriadb">Why ScoriaDB</a></td></tr>
    <tr><td>📊</td><td><a href="#-benchmarks">Benchmarks</a></td></tr>
    <tr><td>📊</td><td><a href="#-how-we-compare-to-redis">Comparison with Redis</a></td></tr>
    <tr><td>🧩</td><td><a href="#-features--capabilities">Features & capabilities</a></td></tr>
    <tr><td>🛡️</td><td><a href="#-durability-and-crash-recovery">Durability & crash recovery</a></td></tr>
    <tr><td>🕰️</td><td><a href="#-how-mvcc-works">How MVCC works</a></td></tr>
    <tr><td>🚀</td><td><a href="#-quick-start">Quick start</a></td></tr>
    <tr><td>📈</td><td><a href="#-mvp-progress">MVP progress</a></td></tr>
    <tr><td>📁</td><td><a href="#-project-structure">Project structure</a></td></tr>
    <tr><td>🗺️</td><td><a href="#-roadmap">Roadmap</a></td></tr>
    <tr><td>📄</td><td><a href="#-license">License</a></td></tr>
    <tr><td>❓</td><td><a href="#-faq">FAQ</a></td></tr>
    <tr><td>🤝</td><td><a href="#-support-the-project">Support the project</a></td></tr>
  </table>
</div>

---

## 📖 What is ScoriaDB?

**ScoriaDB** is an embeddable key‑value database that replaces the need for a combination of networked and embedded storage solutions.
- **As an embedded library** — compiles directly into your Go process, giving you maximum speed with zero external dependencies.
- **As a server** — provides built‑in gRPC, CLI, and (in development) Web UI, without requiring a single line of extra code.

> The project is currently in the **MVP stage**. All core components — LSM engine, MVCC, ACID transactions, Column Families, gRPC/REST API, CLI, and Docker — are implemented and tested. A demo tool **Scorix** for log analysis is currently under development.

---

## 👥 Who is it for

ScoriaDB is useful for:

- **Go developers** who want to add persistent KV storage to their microservice as a library, CLI tool, or agent — without extra infrastructure.
- **IoT and Edge engineers** — when you need local storage on a device but also require remote access and monitoring.
- **Developers using Python, Java, C++ and other languages** — access via gRPC for any language that supports gRPC.
- **Teams with microservice architecture** — one server, many clients in different languages, a single unified API.
- **Anyone who needs a simple persistent KV engine** that deploys in seconds with Docker and offers built‑in CLI and REST/gRPC interfaces.

---

## ✨ Why ScoriaDB?

| Advantage | What it gives you |
| :--- | :--- |
| **Embeddable** | Pure Go, no cgo, a single `import` — a powerful LSM engine inside your process. |
| **Ready‑made server** | gRPC, REST, CLI (and Web UI in development) available immediately, without writing network code. |
| **ACID transactions** | Snapshot Isolation, interactive transactions, atomic WriteBatch. |
| **Column Families** | Independent LSM trees with per‑CF compaction settings and atomicity across them. |
| **MVCC** | Readers never block writers; snapshot reads and (future) Time Travel queries. |
| **Cross‑language access** | 12+ languages via gRPC — Python, Java, C++, Rust, Node.js, C#, and more. |
| **Reliability** | WAL + Manifest with fsync — crash recovery without data loss. |
| **Performance** | ~150 ns reads (MemTable), ~1 µs writes, zero‑copy Value Log for large values. |

---

## 📊 Benchmarks

Measured on Intel Core i3‑1215U (8 threads), Go 1.23+, Linux amd64.  
Run with: `go test -bench=. ./internal/engine ./pkg/scoria`

| Operation | Value size | Time (ns/op) | Throughput |
|---|---|---|---|
| `Put` (small) | < 64 bytes | **1,070 ns** | ~ 935,000 ops/s |
| `Put` (large) | 4 KB (Value Log) | **4,785 ns** | ~ 209,000 ops/s |
| `Get` (hit) | key in MemTable | **~150 ns** | ~ 6,600,000 ops/s |
| `ScoriaPut` | via public API | **1,063 ns** | overhead < 1% |
| `ScoriaGet` | via public API | **144 ns** | overhead ~ 5% |

> Reads are on par with BoltDB (~100–200 ns), but with MVCC, transactions, and concurrent writes.  
> Writes are about 1 million ops/s for single `Put`s without batching.  
> Public API overhead is minimal — the `DB` interface is practically transparent.

---

## 📊 Comparison with Redis

ScoriaDB is **not** a drop‑in replacement for Redis. Redis is a remote, in‑memory data platform; ScoriaDB is an embedded, disk‑backed database.  
The table below shows **local benchmarks** (no network overhead) and **network benchmarks** for Redis to illustrate the architectural difference.

| Feature | ScoriaDB (local) | Redis CE (network) |
| :--- | :--- | :--- |
| **Deployment** | Embedded library or standalone server | Separate server process |
| **Network overhead** | None (embedded mode) | TCP (0.1–0.2 ms+) |
| **Read latency (Get)** | ~150 ns | ~0.24–0.31 ms  |
| **Write latency (Set)** | ~1,070 ns | ~0.45 ms  |
| **Data persistence** | Full (WAL, Manifest, fsync) | Optional (RDB, AOF) |
| **Transactions** | ACID, Snapshot Isolation | None (pipelining) |
| **Multi‑language** | gRPC (12+ languages) | Native protocol (client libraries) |

YCSB benchmarks for ScoriaDB will be published during Release 2.

---

## 🧩 What ScoriaDB offers

### Storage Engine
| Feature | Description | Status |
| :--- | :--- | :--- |
| **LSM tree** | Sorted MemTable (B‑tree) with periodic flush to SSTable on disk. | ✅ |
| **WAL (Write‑Ahead Log)** | Every operation is journaled with a CRC32 checksum before entering the MemTable. | ✅ |
| **Value Log (WiscKey)** | Values > 64 bytes are offloaded to a separate append‑only file; mmap for zero‑copy reads. | ✅ |
| **SSTable** | Block index, key prefix compression, Bloom filter, range filter (min/max key). | ✅ |
| **Leveled Compaction** | Background SSTable merging to reclaim space and remove tombstones. | ✅ |
| **Compression** | Snappy and Zstd at the SSTable block level. | ✅ |
| **GC Value Log** | Garbage collector for the Value Log. | ⚒️ In development |
| **TTL for records** | Automatic expiration of records. | ⏳ Release 2 |
| **Snapshot backup** | Instant snapshots via hardlinks. | ⏳ Release 2 |

### Transactions & versioning
| Feature | Description | Status |
| :--- | :--- | :--- |
| **MVCC** | Multi‑version concurrency control with inverted timestamps. | ✅ |
| **Snapshot Isolation** | Reads see a consistent snapshot at `startTS`; writers never block readers. | ✅ |
| **Interactive transactions** | `Begin()` → `Get`/`Put`/`Delete` → `Commit()`/`Rollback()` with optimistic locking. | ✅ |
| **WriteBatch** | Atomic group of operations under a single `commitTS`. | ✅ |
| **Conflict detection** | At commit time, checks whether any key was changed after `startTS`. | ✅ |

### Data & organisation
| Feature | Description | Status |
| :--- | :--- | :--- |
| **Column Families** | Independent LSM trees with per‑CF compaction settings. Atomic writes across CFs. | ✅ |
| **Secondary indexes** | Automatic index maintenance by key prefixes. | ⏳ Release 2 |
| **Sorted Sets** | Native sorted set support (like Redis). | ⏳ Release 2 |
| **Lists / queues** | Reliable FIFO/LIFO queues with atomic operations. | ⏳ Release 2 |
| **JSON documents** | Automatic serialisation and indexing of JSON. | ⏳ Release 2 |

### Network interfaces & tools
| Feature | Description | Status |
| :--- | :--- | :--- |
| **Embedded Go API** | `DB` and `CFDB` interfaces for direct embedding. | ✅ |
| **gRPC API** | All CRUD operations, Scan streaming, transactions. | ✅ |
| **REST API + WebSocket** | HTTP endpoints for Web UI, push notifications on key changes. | ✅ |
| **CLI client (`scoria`)** | Commands: `get`, `set`, `del`, `scan`, `txn`, `admin`, `inspect`, interactive shell. | ✅ |
| **Web UI (React)** | Dashboard for data browsing and user management. | ⚒️ In development |
| **Authentication** | JWT, bcrypt, roles `admin`/`readwrite`/`readonly`. | ✅ |
| **Docker & docker‑compose** | Multi‑stage server and CLI images, one‑command startup. | ✅ |
| **CI/CD** | GitHub Actions: lint, tests with race detector, license header check. | ✅ |

---

## 🛡️ Durability and crash recovery

The **Manifest** is a metadata journal that records every change to the file set with atomic `fsync`. The **WAL** stores every operation with CRC32. On startup, the engine reads the Manifest to reconstruct the exact file layout and the WAL to recover unflushed writes.

Together they guarantee **full recovery after a sudden power loss** — no metadata corruption, no acknowledged writes lost.

---

## 🕰️ How MVCC works

**MVCC (Multi‑Version Concurrency Control)** means that each write creates a new version of the key instead of overwriting the old one. Each version carries a timestamp (`commitTS`).

1. On `Put`, a new key version is created with `commitTS = <current time>`.
2. When a transaction calls `Begin()`, it gets a `startTS` — a snapshot at that moment.
3. All reads inside the transaction see only versions with `commitTS ≤ startTS`.
4. On `Commit()`, the engine checks whether any key was modified after `startTS` (conflict detection).

**Why this matters:**
- **Writers never block readers.**
- **Snapshot Isolation.** Consistent view of data even under concurrent writes.
- **Time Travel will become possible** (planned for Release 2).

---

## 🚀 Quick start

### Docker (recommended)

```bash
git clone https://github.com/f4ga/scoriadb.git && cd scoriadb
docker compose -f deployments/docker-compose.yml up --build
```

Starts the server (gRPC :50051, HTTP :8080) and runs a CLI integration test.

### Local build

```bash
git clone https://github.com/f4ga/scoriadb.git && cd scoriadb
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli

./scoria-server &   # server on :50051 and :8080

# Get a token (admin/admin is seeded on first start)
TOKEN=$(./scoria-cli --addr localhost:50051 admin auth admin admin)

# CRUD
./scoria-cli --addr localhost:50051 --token "$TOKEN" set hello world
./scoria-cli --addr localhost:50051 --token "$TOKEN" get hello
```

### Run tests (with race detector) and benchmarks

```bash
go test -race ./...
go test -bench=. ./internal/engine ./pkg/scoria
```

---

## 📈 MVP progress

All planned MVP features are implemented and tested:

| Category | Component | Status |
| :--- | :--- | :--- |
| **Core** | LSM tree, Value Log, WAL | ✅ Done |
| | SSTable (Bloom, range filter) | ✅ Done |
| | Manifest, VFS, Compaction | ✅ Done |
| | Basic GC Value Log | ⚒️ In development |
| **Versioning** | MVCC (Snapshot Isolation) | ✅ Done |
| **Transactions** | WriteBatch, Interactive transactions | 🛠️ Fixing bugs |
| **Data** | Column Families | ✅ Done |
| **API** | Embedded Go, gRPC, REST, WebSocket | ✅ Done |
| **Interfaces** | CLI client (`scoria`) | ✅ Done |
| | Web UI (React) | ⚒️ In development |
| **Security** | Authentication (JWT, roles) | ✅ Done |
| **DevOps** | Docker, docker‑compose | ✅ Done |
| **Quality** | CI, benchmarks, linting | ✅ Done |

---

## 📁 Project structure

```
scoriadb/
├── cmd/
│   ├── server/              # gRPC + HTTP server entry point
│   └── cli/                 # CLI client (scoria)
├── internal/
│   ├── engine/              # LSM core: MemTable, SSTable, VLog, WAL, Manifest, compaction
│   │   ├── sstable/         # SSTable read/write, Bloom, encoding
│   │   ├── vfs/             # File system abstraction (testable layer)
│   │   └── tests/           # Engine‑level integration tests
│   ├── mvcc/                # MVCC key encoding with inverted timestamps
│   ├── txn/                 # Transactions (WriteBatch, interactive)
│   ├── cf/                  # Column Families registry + batch
│   └── api/
│       ├── grpc/            # gRPC server implementation
│       ├── rest/            # REST API server
│       └── ws/              # WebSocket hub
├── pkg/scoria/              # Public Embedded Go API (DB, CFDB)
│   └── tests/               # API‑level integration tests
├── proto/                   # Protobuf service definition
├── scoriadb/                # Generated protobuf Go code
├── tests/                   # End‑to‑end & integration tests
├── web/                     # React frontend
└── deployments/             # Docker & docker‑compose
```

---

## 🗺️ Roadmap

### 🟢 Release 1 (MVP) — SOON  
- [x] LSM engine, MVCC, transactions, Column Families
- [ ] Atomic WriteBatch in WAL 🆕
- [ ] Basic GC Value Log 🆕
- [x] Embedded API, gRPC, REST, WebSocket
- [x] CLI and Web client, Docker, CI/CD
- [ ] Demo tool Scorix: log import, search, aggregation, live tail 🆕
- [ ] GitHub Pages documentation

### 🟡 In the future
- **Production readiness:** advanced GC, TTL, backup/restore, lock‑free MemTable.
- **Native data structures:** Sorted Sets, Lists, queues, secondary indexes, JSON documents. 🆕
- **Distributed:** Raft replication and federation without Raft, sharding, distributed ACID transactions (2PC).
- **Security:** mTLS, Row‑Level Security, audit.
- **Observability:** Time Travel queries, Chaos Monkey, OpenTelemetry.

---

## 📄 License

ScoriaDB is distributed under the **Apache License 2.0**.  
The full license text is in the [LICENSE](LICENSE) file.  

Additional terms:  
- Third‑party library notices are listed in [NOTICE](NOTICE).  
- Contributions are governed by the [Contributor License Agreement](CONTRIBUTING.md).  
- All source files contain mandatory license headers; verification is performed automatically in CI.

---

## ❓ FAQ

### 1. Is ScoriaDB a library or a server?
**Both.** You can embed it as a Go library (`import "github.com/f4ga/scoriadb/pkg/scoria"`) or run it as a standalone server with gRPC, REST, and CLI.

### 2. How does it differ from BoltDB?
BoltDB — B+‑tree, single writer, no network. ScoriaDB — LSM, concurrent writes, MVCC, built‑in gRPC/CLI.

### 3. How does it differ from BadgerDB?
Both use WiscKey. BadgerDB is only a library — no server, no interactive transactions. ScoriaDB adds Column Families, MVCC, gRPC, and CLI.

### 4. How does it differ from RocksDB?
RocksDB — C++ (cgo), no built‑in server. ScoriaDB — pure Go, with its own gRPC/CLI and interactive ACID transactions.

### 5. Can I use ScoriaDB from Python/Java/C++?
Yes, via the gRPC API. For embedding you need Go; for all other languages, use network access.

### 6. Does ScoriaDB support SQL?
No. ScoriaDB is a KV database. SQL‑like queries are not planned.

### 7. What happens to data on a sudden power loss?
WAL + Manifest with fsync guarantee full recovery without metadata corruption or data loss.

### 8. Is it production‑ready today?
MVP stage: core engine, transactions, API, and CLI are working, basic GC is coming soon. For critical systems we recommend waiting for Release 2 (backups, TTL, different data types, Raft, 2PC).

### 9. What data structures are currently supported?
Strings only. Release 2 will introduce Sorted Sets, Lists, queues, and indexes.

### 10. Will there be a distributed mode without Raft?
Yes, in Release 2: sharding and federation without mandatory Raft.

### 11. Why gRPC instead of REST?
gRPC is faster (HTTP/2, binary protobuf) and supports streaming. REST is used as an additional interface for Web UI and debugging.

### 12. How can I help the project?
⭐ Star the repo on GitHub, report bugs in Issues, submit Pull Requests, and share the project.

---

## 🤝 Support the project

ScoriaDB is free software. You can help by:

- **⭐️ Starring** the GitHub repo.
- **🐛 Reporting bugs** via [Issues](https://github.com/f4ga/scoriadb/issues).
- **💻 Sending pull requests** — any improvement is welcome.
- **📣 Spreading the word** on social media or in chats.

---

<div align="center">
  <i>Solid as stone. Light as ash.</i><br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>