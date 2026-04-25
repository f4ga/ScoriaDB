
### README.md (English)

```markdown
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
    <tr><td>🌐</td><td><a href="#-multi-language-support">Multi‑language support</a></td></tr>
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

**ScoriaDB** is a hybrid key‑value database that blurs the line between a lightweight embeddable library and a full‑fledged networked DBMS.

- **As an embedded library** — compiles directly into your Go process, giving you maximum speed with zero external dependencies.
- **As a server out of the box** — provides built‑in gRPC, CLI, and Web UI without requiring a single line of extra code.

It’s built so you don’t have to choose between “fast but dumb” embedded databases and “convenient but heavy” networked DBMS.

---

## 👥 Who is it for?

ScoriaDB is useful for:

- **Go developers** who want to add persistent KV storage to their microservice, CLI tool, or agent — without extra infrastructure.
- **IoT and Edge engineers** — when you need local storage on a device but also require remote access and monitoring.
- **Developers using Python, Java, C++** — access data from any language via gRPC without dancing with cgo or writing wrappers.
- **Teams with microservice architecture** — one server, many clients in different languages, a single unified API.
- **Those who build prototypes** — built‑in interfaces (CLI, Web UI) let you see your data immediately.
- **Anyone who needs a simple persistent KV engine without deploying a separate server.**

---

## ✨ Why ScoriaDB?

| Advantage | What it gives you |
| :--- | :--- |
| **Blazing-fast embedded reads** | ~150 ns/op for in-memory `Get` operations. |
| **Hybrid storage (WiscKey)** | Large values don’t bloat the LSM tree; they’re read zero‑copy via mmap. |
| **ACID transactions** | Snapshot Isolation, interactive transactions, atomic WriteBatch. |
| **Built‑in server** | gRPC API, Web UI, and CLI are available immediately — no network code needed. |
| **Cross‑language access** | 12+ languages via gRPC — Python, Java, C++, Rust, Node.js, C#, and more. |
| **Column Families** | Logically separate data with different compaction settings. |
| **Reliability (Manifest + WAL)** | Crash recovery without loss of metadata or data. |
| **Pure Go** | No cgo, no external dependencies — just `go get`. |

---

## 📊 Benchmarks

Measured on Intel Core i3-1215U (8 threads), Go 1.23+, Linux amd64.  
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
> Public API overhead is minimal — the `DB` interface is almost transparent.

---

## 📊 How we compare to Redis

ScoriaDB is **not** a drop-in replacement for Redis. Redis is a remote, in-memory data platform; ScoriaDB is an embedded, disk-backed database.  
The table below shows **local benchmarks** (no network overhead) and **network benchmarks** for Redis to illustrate the architectural difference.

| Feature | ScoriaDB (local) | Redis CE (network) |
| :--- | :--- | :--- |
| **Deployment** | Embedded library or standalone server | Separate server process |
| **Network overhead** | None (embedded mode) | TCP (0.1–0.2 ms+) |
| **Read latency (Get)** | ~150 ns | ~0.24–0.31 ms  |
| **Write latency (Set)** | ~1,070 ns | ~0.45 ms  |
| **Data persistence** | Full (WAL, Manifest, fsync) | Optional (RDB, AOF) |
| **Transactions** | ACID, Snapshot Isolation | None (pipelining) |
| **Multi-language** | gRPC (12+ languages) | Native protocol (client libraries) |

Network benchmarks for ScoriaDB will be published after the gRPC API is fully stabilised (see Roadmap).

---

## 🧩 Features & capabilities

### Storage engine
| Feature | Description |
| :--- | :--- |
| **LSM tree** | Sorted MemTable (B‑tree) with periodic flush to SSTable on disk. |
| **WAL (Write‑Ahead Log)** | Every operation is written to a journal with a CRC32 checksum before entering the MemTable. |
| **Value Log (WiscKey)** | Values > 64 bytes are offloaded to a separate append‑only file; mmap for zero‑copy reads. |
| **SSTable** | Block index, key prefix compression, Bloom filter, range filter (min/max key). |
| **Leveled Compaction** | Background SSTable merging to reclaim space and remove tombstones. |
| **Compression** | Snappy and Zstd at the SSTable block level. |

### Transactions & versioning
| Feature | Description |
| :--- | :--- |
| **MVCC** | Multi‑version concurrency control using inverted timestamps. |
| **Snapshot Isolation** | Reads see a consistent snapshot at `startTS`; writers never block readers. |
| **Interactive transactions** | `Begin()` → `Get`/`Put`/`Delete` → `Commit()`/`Rollback()` with optimistic locking. |
| **WriteBatch** | Atomic group of operations under a single `commitTS`. |
| **Conflict detection** | At commit time, checks whether any key was changed after `startTS`. |

*Note: interactive transactions are fully functional in embedded mode; gRPC‑based transactions are being stabilised.*

### Data & organisation
| Feature | Description |
| :--- | :--- |
| **Column Families** | Independent LSM trees with per-CF compaction settings. Atomic writes across CFs. |
| **Embedded Go API** | Clean `DB` interface in `pkg/scoria` for embedding in Go applications. |

---

## 🛡️ Durability and crash recovery

The **Manifest** is a metadata journal that records every change to the set of files with atomic `fsync`. The **WAL** records every data mutation with CRC32. On startup, the engine reads the Manifest to reconstruct the exact file layout and the WAL to recover unflushed writes.

Together, they guarantee **full recovery after a sudden power loss** — no metadata corruption, no lost writes that were acknowledged.

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
- **Time Travel possible** (planned for Release 2).

---

## 🌐 Multi-language support

ScoriaDB provides a **gRPC API** based on Protocol Buffers. Any language with gRPC support can work with your database.

### 🐹 Go (native)
```go
import "github.com/f4ga/scoriadb/pkg/scoria"

db, _ := scoria.Open(scoria.DefaultOptions("/tmp/mydb"))
defer db.Close()

db.Put([]byte("key"), []byte("value"))
val, _ := db.Get([]byte("key"))
fmt.Println(string(val))
```

### 🐍 Python
```bash
pip install grpcio grpcio-tools
python -m grpc_tools.protoc -I. --python_out=. --grpc_python_out=. scoriadb.proto
```
```python
import grpc
import scoriadb_pb2, scoriadb_pb2_grpc

channel = grpc.insecure_channel('localhost:50051')
stub = scoriadb_pb2_grpc.ScoriaDBStub(channel)
stub.Put(scoriadb_pb2.PutRequest(key=b"user:1", value=b"Alice"))
resp = stub.Get(scoriadb_pb2.GetRequest(key=b"user:1"))
print(resp.value.decode())
```

### ☕ Java
```gradle
dependencies {
    implementation 'io.grpc:grpc-netty-shaded:1.68.0'
    implementation 'io.grpc:grpc-protobuf:1.68.0'
    implementation 'io.grpc:grpc-stub:1.68.0'
}
```
```java
ManagedChannel channel = ManagedChannelBuilder.forAddress("localhost", 50051)
    .usePlaintext().build();
ScoriaDBGrpc.ScoriaDBBlockingStub stub = ScoriaDBGrpc.newBlockingStub(channel);
stub.put(PutRequest.newBuilder()
    .setKey(ByteString.copyFromUtf8("user:1"))
    .setValue(ByteString.copyFromUtf8("Alice")).build());
GetResponse resp = stub.get(GetRequest.newBuilder()
    .setKey(ByteString.copyFromUtf8("user:1")).build());
System.out.println(resp.getValue().toStringUtf8());
```

### 🌍 Supported languages
| Language | Status |
| :--- | :--- |
| Go | ✅ Native API + gRPC |
| Python | ✅ gRPC |
| Java | ✅ gRPC |
| C++ | ✅ gRPC |
| Rust | ✅ via `tonic` |
| Node.js / TypeScript | ✅ gRPC |
| C# (.NET) | ✅ gRPC |
| Ruby | ✅ gRPC |
| PHP | ✅ gRPC |
| Kotlin | ✅ gRPC |
| Swift | ✅ gRPC |
| Dart | ✅ gRPC |

---

## 📈 MVP progress

| Category | Component | Status |
| :--- | :--- | :--- |
| **Core** | LSM tree (MemTable, WAL, Value Log) | ✅ Done |
| | SSTable (Bloom, range filter) | ✅ Done |
| | Manifest (metadata journal) | ✅ Done |
| | VFS abstraction | ✅ Done |
| | Leveled Compaction | ✅ Done |
| **Versioning** | MVCC (Snapshot Isolation) | ✅ Done |
| **Transactions** | WriteBatch | ✅ Done |
| | Interactive transactions | ✅ Done (embedded) |
| **Data organisation** | Column Families | ✅ Done |
| **API** | Embedded Go API | ✅ Done |
| | gRPC API | ✅ Done |
| | REST API + WebSocket | ✅ Done |
| **Interfaces** | CLI client (`scoria`) | ✅ Done |
| | Web UI (React) | 🔜 Next |
| **Security** | Authentication (JWT, roles) | ✅ Done |
| | Rate Limiting | 🔜 Next |
| **Monitoring** | Prometheus metrics | 🔜 Next |
| | Health checks (`/health`, `/ready`) | 🔜 Next |
| **DevOps** | Docker & docker‑compose | 🔜 Next |
| **Quality** | CI/CD (GitHub Actions, linting) | ✅ Done |
| | Benchmarks (engine + API) | ✅ Done |
| | Test structure (unit, integration) | ✅ Done |

---

## 📁 Project structure

```
scoriadb/
├── cmd/
│   ├── server/              # gRPC server entry point
│   └── cli/                 # CLI client (scoria)
├── internal/
│   ├── engine/              # LSM core: MemTable, SSTable, VLog, WAL, Manifest, compaction
│   │   ├── sstable/         # SSTable read/write, Bloom filter, encoding
│   │   ├── vfs/             # file system abstraction (testable FS layer)
│   │   └── tests/           # engine-level integration tests
│   ├── mvcc/                # MVCC key encoding with inverted timestamps
│   ├── txn/                 # transactions (WriteBatch, interactive)
│   ├── cf/                  # Column Families registry + batch
│   └── api/
│       ├── grpc/            # gRPC server implementation
│       ├── rest/            # REST API server
│       └── ws/              # WebSocket hub
├── pkg/scoria/              # public Embedded Go API (DB interface)
│   └── tests/               # API-level integration tests
├── proto/                   # protobuf service definition
├── scoriadb/                # generated protobuf Go code
├── tests/                   # end-to-end & integration tests
├── web/                     # React frontend (planned)
└── deployments/             # Docker & docker-compose (planned)
```

---

## 🗺️ Roadmap

### Current release: v1.0 MVP
- [x] Core LSM engine with WiscKey
- [x] MVCC + ACID transactions
- [x] Column Families
- [x] Embedded Go API
- [x] gRPC API
- [x] CLI client
- [x] CI/CD with benchmarks

### Release 2
- [ ] **GC Value Log** — garbage collector for dead entries in the Value Log.
- [ ] **YCSB benchmarks** — comparison with BadgerDB, PebbleDB, etcd, and Redis (network measurements).
- [ ] **Raft replication** — distributed mode with sharding.
- [ ] **Time Travel queries** — read keys as of any past timestamp.
- [ ] **Chaos Monkey** — fault injection tests for Raft.
- [ ] **GitHub Pages documentation** — concise developer guide.

### Release 3
- [ ] **Enterprise security:** Row-Level Security (RLS), mTLS, auditing.
- [ ] **Kubernetes:** Operator and Helm chart.
- [ ] **Advanced features:** Git‑like branching, AI assistant.
- [ ] **Business model:** Dual licensing (Apache 2.0 / BSL).

---

## 📄 License

ScoriaDB is distributed under the **Apache License 2.0**.  
The full license text is in the [LICENSE](LICENSE) file.  

Additional terms:  
- Third-party library notices are in [NOTICE](NOTICE).  
- Contributions are governed by the [Contributor License Agreement](CONTRIBUTING.md).  
- All source files contain mandatory license headers; verification is performed automatically in CI.  

Enterprise features (Raft replication, Time Travel, mTLS, Row-Level Security, Kubernetes operator) will be available under a separate commercial license.  
For commercial licensing inquiries: [your email].

---

## ❓ FAQ

### 1. Is ScoriaDB a library or a server?
**Both.** You can embed ScoriaDB as a Go library (`import "github.com/f4ga/scoriadb/pkg/scoria"`) or run it as a server with gRPC and Web UI access. Both modes run from the same binary.

### 2. How does ScoriaDB differ from BoltDB?
BoltDB uses a B+ tree, allows only one writer at a time, and has no built‑in network access. ScoriaDB is LSM‑based, supports concurrent writes, MVCC with Snapshot Isolation, and provides gRPC/CLI/Web UI out of the box.

### 3. How does ScoriaDB differ from BadgerDB?
Both use WiscKey (Value Log). BadgerDB is only an embedded library — no server, no interactive transactions (only batch). ScoriaDB adds interactive transactions, Column Families, Manifest, and cross‑language gRPC access.

### 4. Why is ScoriaDB faster than Redis in local benchmarks but slower over the network?
Redis is a remote server. Every request traverses TCP and the network stack, adding ~0.1–0.2 ms of latency. ScoriaDB in embedded mode runs inside your process — a `Get` is a direct function call. Network benchmarks for ScoriaDB will be published once the gRPC API is fully stable, enabling a fair comparison.

### 5. Can I use ScoriaDB in production?
The project is in MVP stage. The core passes CI with the race detector, and critical structures (Manifest, WAL) are covered by tests. It can already be tried for non‑critical projects; for heavy workloads we recommend waiting for the first stable release.

### 6. How do interactive transactions work over gRPC?
In embedded mode interactive transactions are fully functional. gRPC‑based transactions are being stabilised: `BeginTxn`, `CommitTxn`, `RollbackTxn` are available but still require additional testing under concurrent load.

---

## 🤝 Support the project

ScoriaDB is free software under the Apache 2.0 license. You can help by:

- **⭐️ Starring** the GitHub repo — it’s great motivation!
- **🐛 Reporting bugs** via [Issues](https://github.com/f4ga/scoriadb/issues).
- **💻 Sending pull requests** — any improvement is welcome.
- **📣 Spreading the word** about the project on social media or in chats.

Thank you for being here!

---

<div align="center">
  <i>Solid as stone. Light as ash.</i><br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>
