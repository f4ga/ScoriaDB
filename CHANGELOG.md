# Changelog

All notable changes to ScoriaDB will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-05-26

### Added

#### Group Commit in WAL
- Implemented `groupCommitWriter` with buffered writes and periodic flush (10 ms interval)
- Added `WALOptions` struct to configure Group Commit behavior:
  - `GroupCommitEnabled` – enable/disable buffered writes
  - `GroupCommitInterval` – flush interval (default 10 ms)
  - `MaxBufferSize` – optional buffer size limit before forced flush
- Extended `NewLSMEngine` with variadic `...WALOptions` parameter
- Added `OpenWALWithOptions` function to create WAL with custom settings
- Added `WAL.Flush()` method to force buffer flush when needed

#### Public API Extensions
- Added `Options` struct in `pkg/scoria` with `WALGroupCommit` field
- Added `NewScoriaDBWithOptions` constructor
- All existing `NewScoriaDB` calls remain backward‑compatible

#### Benchmarks
- Added WAL benchmarks:
  - `BenchmarkWALWrite_Sync` – sequential writes with fsync each op
  - `BenchmarkWALWrite_GroupCommit` – sequential writes with group commit
  - `BenchmarkWALWriteParallel_Sync` – concurrent writes with fsync each op
  - `BenchmarkWALWriteParallel_GroupCommit` – concurrent writes with group commit
- Added Put benchmarks for public API:
  - `BenchmarkPutSmallSync` / `BenchmarkPutSmallGroupCommit` (16 bytes)
  - `BenchmarkPutLargeSync` / `BenchmarkPutLargeGroupCommit` (4 KB)
- Added benchmark for different value sizes (16B, 256B, 1KB, 4KB, 16KB, 64KB)

#### Documentation
- Updated `README.md` and `README_RU.md` with:
  - Benchmark tables (sync vs group commit, read throughput)
  - Group Commit explanation and performance impact
  - v0.2.0 release features
- Updated `docs/README.md` to match main README
- Added comparison tables with Redis, BadgerDB, Pebble

### Changed

- **Group Commit is now the default mode in server** (`scoria-server`)
- WAL write throughput improved by **4-5×** for sequential writes (from 2.2M to 10.5M ops/s)
- Small value write (16B) with Group Commit: **1.43M ops/s** (vs 888K sync)
- Large value write (4KB) with Group Commit: **867 MB/s** (vs 840 MB/s sync)
- Read performance remains unchanged: **6.6M ops/s** (hit), **8.3M ops/s** (miss)

### Fixed

- No critical bug fixes in this release

### Known Limitations (v0.2.0)

| Limitation | Planned fix |
|------------|--------------|
| MemTable uses B‑tree with global mutex | lock‑free skip list – v0.4.0 |
| Manifest stored as JSON | binary format – v0.3.0 |
| Value Log GC is manual only (`scoria gc`) | automatic incremental GC – v0.4.0 |
| Transactions work only on `default` CF | v0.3.0 |
| No true zero‑copy (data copied from mmap) | v0.4.0 |

### Performance Summary (Intel Core i3-1215U, NVMe SSD, Go 1.23)

| Operation | Mode | Value size | Time (ns/op) | Throughput (ops/s) |
|-----------|------|------------|--------------|--------------------|
| `engine.Put` | Sync | 16 B | 1 070 | ~935 000 |
| `engine.Put` | Sync | 4 KB | 4 785 | ~209 000 |
| `engine.Get` (hit) | – | – | 152 | ~6 580 000 |
| `engine.Get` (miss) | – | – | 310 | ~3 225 000 |
| `WAL.Write` | Sync | ~50 B | 418 | ~2 390 000 |
| `WAL.Write` | Group Commit | ~50 B | **94.9** | **~10 540 000** |
| `Put` (public API) | Sync | 16 B | 1 126 | ~888 000 |
| `Put` (public API) | Group Commit | 16 B | **~700** | **~1 430 000** |
| `Put` (public API) | Group Commit | 4 KB | **~4 500** | **~222 000** |

---

## [0.1.1] - 2026-05-18

### Added
- CLI / Interactive shell commands:
  - `create-cf`, `list-cf`, `delete-cf` – Column Family management
  - `whoami` – show current user and roles (JWT decoding)
  - `stats` – key count in current CF
  - `history`, `clear`, `last-error`, `export`
  - `admin change-password`, `admin user-add`, `admin list-users`
- gRPC API extensions:
  - `ListCF`, `CreateCF`, `DeleteCF`, `ListUsers`, `ChangePassword`
- Full documentation for Python, Java, C++ clients (guides + examples)
- CLI demo screenshot (`docs/cli-demo.png`)

### Fixed
- `ChangePassword` now rejects empty passwords
- Syntax errors and duplicate methods in `shell.go`
- License check now ignores `docs/` folder
- `gofmt -s` applied to all files (Go Report Card → A)

### Removed
- Temporary files: `cli`, `NOLINT.md`, `TECHDEBT.md`, `plan1.md`

---

## [0.1.0] - 2026-04-30

### Added
- LSM engine (MemTable, SSTable, Leveled Compaction, Bloom filter, prefix compression)
- WAL with CRC32 and fsync crash recovery
- Value Log (WiscKey) for large values (>64 bytes) with mmap
- MVCC with Snapshot Isolation (inverted timestamps)
- Interactive ACID transactions and atomic WriteBatch
- Column Families (independent LSM trees, shared WAL)
- Embedded Go API (`DB` and `CFDB` interfaces)
- gRPC server (CRUD, streaming Scan, transactions, JWT auth)
- REST API (Gin) and WebSocket hub
- CLI (`scoria`) – `get`, `set`, `del`, `scan`, `txn`, admin commands
- Docker and docker‑compose setup
- GitHub Actions CI (lint, race detector, license check)
- Prometheus metrics and health/ready endpoints
- Basic manual GC for Value Log (`scoria gc`)
- Fail‑safe VLog recovery on magic mismatch
- VFS abstraction for disk failure testing
- Stress tests (concurrent writes, transaction conflicts, compaction under load)

### Performance (Intel Core i3-1215U, NVMe SSD, Go 1.23+)

| Operation | Value size | Time (ns/op) | Throughput (ops/s) |
|-----------|------------|--------------|--------------------|
| `engine.Put` (small) | <64 B | 1 070 | ~935 000 |
| `engine.Put` (large) | 4 KB | 4 785 | ~209 000 |
| `engine.Get` (hit) | – | 152 | ~6 580 000 |
| `WAL.Write` (sync) | ~50 B | 418 | ~2 390 000 |

---

## [0.0.1] - 2026-04-22

### Added
- Initial prototype: basic LSM engine with MemTable (B‑tree) and SSTable
- Simple WAL without fsync
- Value Log prototype
- Basic CLI (limited commands)

---

**Legend:**
- ✅ – Implemented and stable
- 🛠️ – In development (planned for next releases)
- ⏳ – Planned (no specific version)

For detailed documentation, see [README.md](README.md) and [docs/](docs/).