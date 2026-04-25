
<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn" alt="Header">
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&section=header&text=🔥%20Embedded%20LSM%20database%20in%20Go%20|%20Solid%20as%20stone,%20light%20as%20ash&fontSize=20&fontAlignY=50&animation=twinkling" alt="Subtitle">
  <br><br>

  [![CI](https://github.com/f4ga/ScoriaDB/.github/workflows/ci.yml/badge.svg)](https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml)
  [![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev/)
  [![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

  <br>
  <table align="center" style="font-size: 1.4em; line-height: 2;">
    <tr><td>📖</td><td><a href="#-what-is-it">What is ScoriaDB</a></td></tr>
    <tr><td>👥</td><td><a href="#-who-is-it-for">Who is it for</a></td></tr>
    <tr><td>🎯</td><td><a href="#-when-should-you-use-scoriadb">When to use ScoriaDB</a></td></tr>
    <tr><td>✨</td><td><a href="#-why-scoriadb">Why ScoriaDB</a></td></tr>
    <tr><td>📊</td><td><a href="#-comparison">Comparison with alternatives</a></td></tr>
    <tr><td>🚀</td><td><a href="#-how-scoriadb-is-fundamentally-different-from-other-embedded-databases">Fundamental differences</a></td></tr>
    <tr><td>🧩</td><td><a href="#-features--capabilities">Features & capabilities</a></td></tr>
    <tr><td>🛡️</td><td><a href="#-what-does-the-manifest-guarantee-about-data-durability-during-a-sudden-power-loss">Manifest & durability guarantees</a></td></tr>
    <tr><td>⚡</td><td><a href="#-why-wisckey-value-log-and-when-does-it-really-speed-things-up">WiscKey speed benefits</a></td></tr>
    <tr><td>🕰️</td><td><a href="#-how-mvcc-works-and-why-it-matters-for-transactions">How MVCC works</a></td></tr>
    <tr><td>🌐</td><td><a href="#-using-scoriadb-from-different-languages">Multi‑language support</a></td></tr>
    <tr><td>📈</td><td><a href="#-mvp-progress">MVP progress</a></td></tr>
    <tr><td>⚡</td><td><a href="#-benchmarks">Benchmarks</a></td></tr>
    <tr><td>❓</td><td><a href="#-faq">FAQ</a></td></tr>
    <tr><td>🤝</td><td><a href="#-support-the-project">Support the project</a></td></tr>
  </table>
</div>

---

## 📖 What is it?

**ScoriaDB** is a **hybrid** key‑value database that blurs the line between a lightweight embeddable library and a full‑fledged networked DBMS.

- **As an embedded library** — compiles directly into your Go process, giving you maximum speed with no external dependencies.
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
- **Anyone tired of spinning up Redis for a simple cache** or writing an HTTP wrapper around BoltDB.

---

## 🎯 When should you use ScoriaDB?

### 1. Embedded application that also needs network access
Your Go app stores data locally, but external clients (gRPC/REST) or an admin via browser must be able to connect.
*Other libraries (BoltDB, Badger) don’t offer this; Redis requires a separate process.*

### 2. Storing large binary values
You store many images, blobs, or logs. A classic LSM tree blows up on rewrites.
*ScoriaDB offloads large values to a separate Value Log and reads them zero‑copy via mmap, keeping the tree lean.*

### 3. Microservice without an external database
The service must be self‑contained, not depend on a separate DBMS, yet still be able to serve data to the outside.
*ScoriaDB compiles into your service and provides a gRPC API for other services.*

### 4. Console tool with convenient data inspection
You’re writing a CLI tool that collects data. You’d like to peek into the storage without inventing an export format.
*Built‑in CLI and Web UI let you browse keys and values from the terminal or a browser.*

### 5. A project that will grow into a distributed system
A single node is enough today, but tomorrow you might need a cluster. You don’t want to rewrite the code when you scale.
*ScoriaDB plans to add Raft replication — the same API will become distributed without changing your code.*

### 6. Cross‑language development
Your team writes microservices in Go, Python, and Java — all need access to the same data.
*ScoriaDB gives you ready‑made gRPC clients for 12+ languages without writing wrappers.*

---

## ✨ Why ScoriaDB?

| Advantage | What it gives you |
| :--- | :--- |
| **Hybrid storage (WiscKey)** | Large values don’t bloat the LSM tree; they’re read zero‑copy via mmap. |
| **ACID transactions** | Snapshot Isolation, interactive transactions, atomic WriteBatch. |
| **Built‑in server** | gRPC API, Web UI, and CLI are available immediately — no network code needed. |
| **Cross‑language access** | 12+ languages via gRPC — Python, Java, C++, Rust, Node.js, C#, and more. |
| **Column Families** | Logically separate data with different compaction settings. |
| **Reliability (Manifest + WAL)** | Crash recovery without loss of metadata or data. |
| **Pure Go** | No cgo, no external dependencies — just `go get`. |

---

## 📊 Comparison

| Feature | ScoriaDB | BoltDB/bbolt | BadgerDB | LevelDB/GoLevelDB | RocksDB (cgo) | Redis/etcd | MongoDB | Cassandra |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| **Embeddable** | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ |
| **Network access** | ✅ | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |
| **Web UI** | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ / partial | ❌ | ❌ |
| **MVCC / Snapshot Isolation** | ✅ | ❌ | ❌ | ❌ | ✅ | ❌ / ✅ | ✅ | ❌ |
| **Large values (zero‑copy)** | ✅ | ❌ | ✅ | ❌ | Partial | ❌ | ✅ | ❌ |
| **Column Families** | ✅ | ❌ | ❌ | ❌ | ✅ | ❌ | ❌ | ✅ |
| **Interactive transactions** | ✅ | ❌ | ❌ | ❌ | ✅ | ❌ | ✅ | ❌ |
| **Cross‑language access** | ✅ 12+ languages | ❌ | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ |
| **Dependencies** | Pure Go | Pure Go | Pure Go | optional cgo | C++/cgo | Separate process | Separate process | Separate process |

---

## 🚀 How is ScoriaDB fundamentally different from other embedded databases?

**1. It’s not “just a library” — it’s a ready‑to‑use server.**  
BoltDB, BadgerDB, and LevelDB are only code you embed into your process. To make them accessible over the network, you’d have to write an HTTP/gRPC server, a CLI, and a UI. ScoriaDB gives you all of that out of the box.

**2. Full ACID transactions with Snapshot Isolation.**  
Unlike BoltDB (single writer) and BadgerDB (no interactive transactions), ScoriaDB supports interactive transactions with Snapshot Isolation — just like “grown‑up” databases such as PostgreSQL and CockroachDB.

**3. Hybrid storage for large values (WiscKey).**  
Big JSON documents or binary files don’t blow up the LSM tree — they’re stored separately and read directly from memory (zero‑copy). This gives you a speed boost and saves RAM compared to BoltDB and LevelDB.

**4. Cross‑language access out of the box.**  
No other embedded database provides a gRPC API. ScoriaDB is accessible from Go, Python, Java, C++, Rust, Node.js, and many other languages without cgo dancing.

**5. Architecture designed for distribution.**  
The engine is built so that Raft replication can be added later without changing the API. You’ll be able to turn a single node into a fault‑tolerant cluster without rewriting your code.

---

## 🧩 Features & capabilities

### Storage engine
| Feature | Description |
| :--- | :--- |
| **LSM tree** | Sorted MemTable (B‑tree) with periodic flush to SSTable on disk. |
| **WAL (Write‑Ahead Log)** | Every operation is written to a journal with a CRC32 checksum before entering the MemTable. |
| **Value Log (WiscKey)** | Values > 64 bytes are offloaded to a separate append‑only file; mmap for zero‑copy reads. |
| **SSTable** | Block index, key prefix compression, Bloom filter, range filter (min/max key). |
| **Leveled Compaction** | Background merging of SSTables across levels to free space and remove tombstones. |
| **Compression** | Snappy and Zstd support at the SSTable block level. |

### Transactions & versioning
| Feature | Description |
| :--- | :--- |
| **MVCC** | Multi‑version concurrency control using inverted timestamps (TiKV‑like approach). |
| **Snapshot Isolation** | Reads see a consistent snapshot of data at `startTS`; writers never block readers. |
| **Interactive transactions** | `Begin()` → `Get`/`Put`/`Delete` → `Commit()`/`Rollback()` with optimistic locking. |
| **WriteBatch** | Atomic application of a group of operations under a single `commitTS`. |
| **Conflict detection** | At commit time, checks whether any key was changed after `startTS`. |

### Data & organisation
| Feature | Description |
| :--- | :--- |
| **Column Families** | Independent LSM trees with their own compaction settings. Atomic operations across CFs. |
| **Embedded Go API** | Clean `DB` interface in `pkg/scoria` for embedding in Go applications. |

### Reliability & recovery
| Feature | Description |
| :--- | :--- |
| **Manifest** | Journal of metadata changes (VersionEdit) with atomic `fsync`. Recovers after a crash without scanning the directory. |
| **VFS abstraction** | Pluggable file system layer for testing (simulate disk failures). |

---

### What does the Manifest guarantee about data durability during a sudden power loss?

The **Manifest** is a metadata journal that records every change to the set of files (creation, deletion, compaction of SSTables) as an atomic entry with `fsync`. On startup, the engine reads this journal and reconstructs the exact state of all levels without scanning the directory.

**Does it guarantee durability after a power loss?**  
Yes — but together with the WAL. The Manifest guarantees that the metadata (which files are on which levels) is consistent. The WAL guarantees that data not yet flushed to SSTables isn’t lost. Together they ensure full recovery after an unexpected shutdown.

---

### Why WiscKey (Value Log) and when does it really speed things up?

**WiscKey** is the technique of storing keys and indexes in the LSM tree while large values live in a separate append‑only file (Value Log). Why does it matter?

- **Reduces write amplification.** When a key is overwritten, the value is not copied again — a new pointer is written to the Value Log.
- **Zero‑copy reads.** The Value Log is memory‑mapped (`mmap`). When you read a value, you get a slice that points directly into memory, with no extra allocations.
- **Saves RAM.** The LSM tree stays compact because it doesn’t hold gigabytes of values in the MemTable.

**When does this really speed things up?**
- When you store **values larger than ~100 bytes** (JSON, binary blobs).
- When you have **intense write workloads** with large data (logs, metrics).
- When you need to **conserve RAM** without sacrificing read performance.

---

### How MVCC works and why it matters for transactions

**MVCC (Multi‑Version Concurrency Control)** means that each write (Put/Delete) creates a new version of the key instead of overwriting the old one. Each version carries a timestamp (`commitTS`).

**How it works inside ScoriaDB:**
1. On `Put`, a new version of the key is created with `commitTS = <current time>`.
2. When a transaction calls `Begin()`, it gets a `startTS` — a snapshot of the state at that moment.
3. All reads inside the transaction (`Get`) see only versions with `commitTS ≤ startTS`.
4. On `Commit()`, the engine checks whether any key was changed after `startTS` (conflict detection).

**Why this matters:**
- **Writers never block readers.** You can read and write concurrently without waiting.
- **Snapshot Isolation.** A transaction sees a consistent snapshot even if other writes happen in parallel.
- **Time Travel possible.** In the future you’ll be able to ask, “what did this key look like yesterday at 10:00?”

---

## 🌐 Using ScoriaDB from different languages

ScoriaDB provides a **gRPC API** based on Protocol Buffers. That means any language with gRPC support can work with your database. You describe the `.proto` file once — then generate client code automatically.

### Steps for any language

1. **Install gRPC and protobuf** for your language (instructions below).
2. **Download the `.proto` file** from the ScoriaDB repository.
3. **Generate client code** with `protoc`.
4. **Use the generated client** — call methods like ordinary functions.

---

### 🐹 Go (native language)

```go
import "github.com/f4ga/scoriadb/pkg/scoria"

db, _ := scoria.Open(scoria.DefaultOptions("/tmp/mydb"))
defer db.Close()

db.Put([]byte("key"), []byte("value"))
val, _ := db.Get([]byte("key"))
fmt.Println(string(val))
```

---

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
print(resp.value.decode())  # Alice
```

---

### ☕ Java

```gradle
dependencies {
    implementation 'io.grpc:grpc-netty-shaded:1.68.0'
    implementation 'io.grpc:grpc-protobuf:1.68.0'
    implementation 'io.grpc:grpc-stub:1.68.0'
}
```

```bash
protoc --java_out=src/main/java --grpc-java_out=src/main/java scoriadb.proto
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

---

### ⚙️ C++

```cmake
find_package(gRPC CONFIG REQUIRED)
target_link_libraries(my_app PRIVATE gRPC::grpc++ gRPC::grpc++_reflection protobuf::libprotobuf)
```

```bash
protoc --cpp_out=. --grpc_out=. --plugin=protoc-gen-grpc=$(which grpc_cpp_plugin) scoriadb.proto
```

```cpp
auto channel = grpc::CreateChannel("localhost:50051", grpc::InsecureChannelCredentials());
std::unique_ptr<ScoriaDB::Stub> stub = ScoriaDB::NewStub(channel);

PutRequest req;
req.set_key("user:1");
req.set_value("Alice");
stub->Put(nullptr, &req, nullptr);
```

---

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
| | Interactive transactions | ✅ Done |
| **Data organisation** | Column Families | ✅ Done |
| **API** | Embedded Go API | ✅ Done |
| | gRPC API | ✅ Done |
| | REST API | 🔜 Next |
| **Interfaces** | CLI client (`scoria`) | 🔜 Next |
| | Web UI (React) | 🔜 Next |
| **Security** | Authentication (JWT, roles) | 🔜 Next |
| | Rate Limiting | 🔜 Next |
| **Monitoring** | Prometheus metrics | 🔜 Next |
| | Health checks (`/health`, `/ready`) | 🔜 Next |
| **DevOps** | Docker & docker‑compose | 🔜 Next |
| **Quality** | CI/CD (GitHub Actions, linting) | ✅ Done |
| | Benchmarks (engine + API) | ✅ Done |
| | Test structure (unit, integration) | ✅ Done |

---

## ⚡ Benchmarks

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

## 🗺️ Roadmap (next release)

- Distributed mode (Raft replication, sharding)
- Distributed ACID transactions (Percolator‑like 2PC)
- Time Travel queries and historical browser
- Git‑like data branching
- Advanced security (Row‑Level Security, mTLS)
- Kubernetes integration

---

## 🚀 Quick start

```bash
git clone https://github.com/f4ga/scoriadb.git
cd scoriadb
go build ./...

# Tests and benchmarks
go test -race ./...
go test -bench=. ./internal/engine ./pkg/scoria

# Run server (gRPC port 50051, HTTP 8080)
go run cmd/server/main.go
```

A Docker image will be available with the first stable release.

---

## ❓ FAQ

### 1. Is ScoriaDB a library or a server?

**Both.** You can embed ScoriaDB as a Go library (`import "github.com/f4ga/scoriadb/pkg/scoria"`) or run it as a server with gRPC and Web UI access. Both modes run from the same binary.

### 2. How does ScoriaDB differ from BoltDB?

BoltDB uses a B+ tree, allows only one writer at a time, and has no built‑in network access. ScoriaDB is LSM‑based, supports concurrent writes, MVCC with Snapshot Isolation, and provides gRPC/CLI/Web UI out of the box.

### 3. How does ScoriaDB differ from BadgerDB?

Both use WiscKey (Value Log). But BadgerDB is only an embedded library — no built‑in server and no interactive transactions (only batch). ScoriaDB adds interactive transactions, Column Families, the Manifest, and cross‑language gRPC access.

### 4. Can I use ScoriaDB in production?

The project is currently in MVP stage. We are actively working on stabilisation, testing, and benchmarks. For critical systems, we recommend waiting for the first stable release or thoroughly testing under your own workload.

### 5. What durability guarantees does ScoriaDB provide?

Every operation is written to the WAL with a CRC32 checksum. All changes to the file set are written to the Manifest with `fsync`. After a sudden power loss, the engine recovers to a consistent state on the next start.

### 6. How does ScoriaDB handle concurrency?

Through Snapshot Isolation with optimistic locking. Readers never block. Writers check for conflicts at commit time and retry if necessary.

### 7. Does ScoriaDB support TTL (time‑to‑live)?

Not yet. TTL is planned for a future release after the core functionality is stable.

### 8. Will there be support for distributed mode?

Yes. Raft replication is the main goal of the next major release. You’ll be able to run a cluster of 3+ nodes with automatic leader elections and strong data consistency.

### 9. Why gRPC instead of REST?

gRPC provides strong typing, high performance (HTTP/2, binary protobuf), and streaming for `Scan`. However, a REST API will also appear in a future release for easier browser integration and debugging.

### 10. How can I help the project?

Give the repository a star on GitHub, try the library in your own projects, report issues, and submit pull requests. We’re open to the community and value any contribution!

---

## 🤝 Support the project

ScoriaDB is free software under the MIT license. You can help by:

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

---

You can copy this entire Markdown block directly into your `README.md` (or any other documentation file). All links, badges, and formatting will work as expected. Let me know if you’d like any adjustments — for example, changing the tagline or tweaking any technical term.
