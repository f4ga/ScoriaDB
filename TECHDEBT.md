# 🛠️ ScoriaDB — Technical Debt

> Last Updated: 22.06.2026
> Priority: Critical issues that affect stability, reliability, and production readiness.

---

## 🔴 Critical: Unhandled Errors

| File | Line | Call | Issue | Target |
|------|------|------|-------|--------|
| `internal/engine/compaction.go` | 39,44,51,59,91 | `_ = e.vfs.Remove(...)` | No error logging for SSTable removal | v0.4.0 |
| `internal/engine/flush.go` | 58,64,71,79,100 | `_ = e.vfs.Remove(...)` | No error logging for SSTable removal | v0.4.0 |
| `internal/api/rest/server.go` | 179 | `_ = json.NewEncoder(w).Encode(data)` | JSON encoding error ignored | v0.4.0 |
| `internal/api/rest/server.go` | 186 | `_ = json.NewEncoder(w).Encode(...)` | JSON encoding error ignored | v0.4.0 |
| `cmd/server/main.go` | 71 | `_, _ = w.Write([]byte("OK"))` | HTTP write error ignored | v0.4.0 |
| `cmd/server/main.go` | 81 | `_, _ = w.Write([]byte("READY"))` | HTTP write error ignored | v0.4.0 |

---

## 🔴 Critical: Missing Implementation

| File | Function/Method | Issue | Target |
|------|-----------------|-------|--------|
| `internal/api/grpc/server.go` | `CreateUser` | Stub — returns `Unimplemented` | v0.3.0 |
| `internal/api/grpc/server.go` | `Authenticate` | Stub — returns `Unimplemented` | v0.3.0 |
| `internal/engine/lsm.go` | `GetWithTS` (key not found) | Returns `nil, nil` instead of error | v0.4.0 |
| `internal/engine/lsm.go` | `Scan` | Returns `nil, nil` — no iterator | v0.3.0 |
| `internal/api/grpc/server.go` | `CommitTxn` | Doesn't apply operations from `req.GetOps()` | v0.3.0 |
| `pkg/scoria/scoriadb.go` | `Scan` | Not implemented — no MemTable + SSTable merge | v0.3.0 |

---

## 🔴 Critical: TODO (Must Fix)

| File | Line | TODO | Target |
|------|------|------|--------|
| `internal/engine/compaction.go` | 34 | `// TODO: реализовать настоящее слияние` — Implement proper SSTable merge | v0.4.0 |
| `internal/engine/lsm.go` | 73 | `// TODO: восстановить из данных` — Restore last timestamp from data | v0.4.0 |
| `internal/api/grpc/server.go` | 67,83 | `// TODO: return actual commit timestamp` | v0.3.0 |
| `internal/api/grpc/server.go` | 124 | `// TODO: generate actual start timestamp` | v0.3.0 |
| `internal/api/grpc/server.go` | 190 | `// TODO: use proper UUID` — Use proper UUID for transaction ID | v0.3.0 |
| `internal/txn/transaction.go` | 46 | `// TODO: реализовать атомарное получение следующего timestamp` | v0.4.0 |
| `internal/txn/transaction.go` | 120 | `// TODO: реализовать проверку конфликтов` — Implement conflict detection | v0.4.0 |
| `internal/cf/registry.go` | 25 | `// TODO: передавать VFS в опциях` — Pass VFS in CF options | v0.4.0 |
| `internal/cf/batch.go` | 71 | `// TODO: улучшить атомарность` — Improve batch atomicity | v0.4.0 |
| `internal/cf/batch.go` | 132 | `// TODO: реализовать откат` — Implement batch rollback | v0.4.0 |

---

## 🟡 Important: Dependencies to Update

| Dependency | Current Version | Reason |
|------------|-----------------|--------|
| `github.com/google/btree` | v1.1.3 | ⚠️ **Will be removed in v0.3.0** (replaced by skip list) |
| `google.golang.org/grpc` | v1.80.0 | Security + new features |
| `google.golang.org/protobuf` | v1.36.11 | Compatibility |
| `golang.org/x/net` (indirect) | v0.49.0 | Security |
| `golang.org/x/sys` (indirect) | v0.40.0 | Security |
| `golang.org/x/text` (indirect) | v0.33.0 | Security |
| `google.golang.org/genproto/googleapis/rpc` (indirect) | v0.0.0-202601... | Compatibility |

---

## 🟡 Important: Stubs & Placeholders

| File | Function/Method | Current Behavior | Expected |
|------|-----------------|------------------|----------|
| `internal/txn/transaction.go` | `applyOp` (unknown op) | `return nil, nil` | Handle unknown operation |
| `internal/engine/sstable/bloom.go` | `optimalBitsPerKey` | Unused | Planned for dynamic Bloom |

---

## ✅ Closed Items

| ID | Problem | Solution | Date | Status |
|----|---------|----------|------|--------|
| TD-REF-01 | Duplicated WriteBatch, compareKeys, constants | Created `internal/keys/keys.go` with unified `CompareKeys`. Removed `internal/engine/batch.go`, `internal/engine/batch_test.go`. Removed `maybeCompactLevel0`, `maybeFlush`. | 20.06.2026 | ✅ CLOSED |
| TD-ERR-01 | 50+ errcheck warnings | Created `internal/errors/close.go` with `CloseWithLog`/`CloseWithFatal`. Replaced all `defer x.Close()` across 30+ files. | 20.06.2026 | ✅ CLOSED |
| TD-DEAD-01 | Dead code (80+ unused functions) | Removed dead code, unused dependencies, cleaned up `go.mod` | 20.06.2026 | ✅ CLOSED |
| TD-DEAD-02 | Dead files: `internal/api/notify.go`, `internal/api/ws/` | Removed `NotifyingDB` wrapper and WebSocket hub (both unused). Removed `gorilla/websocket` dependency via `go mod tidy`. | 20.06.2026 | ✅ CLOSED |
| TD-POOL-01 | High allocations in encodeWalEntry (20%) and mergeIterator.Next (6%) | Added `sync.Pool` for `WalEntry`, `heapItem`, and encode buffers. Replaced all `&WalEntry{}` with `newWalEntry()`/`putWalEntry()` in `lsm.go` and `wal.go`. Replaced all `&heapItem{}` with `newHeapItem()`/`putHeapItem()` in `merge_iterator.go`. | 20.06.2026 | ✅ CLOSED |
| TD-LOG-01 | Logging without levels and context | Created `internal/logger/logger.go` with DEBUG/INFO/WARN/ERROR levels, source location, and component name. Replaced all `log.Printf`/`log.Println`/`log.Fatal` across 10 files. | 20.06.2026 | ✅ CLOSED |
| TD-CRASH-01 | Insufficient crash-recovery tests | Added `internal/engine/crash_test.go` with 6 tests: `TestCrashDuringCompaction`, `TestCorruptedWAL`, `TestCorruptedManifest`, `TestCrashDuringFlush`, `TestCorruptedWAL_Truncated`, `TestCorruptedManifest_Empty`. Fixed `stopBackgroundTasks` nil channel panic. Made WAL `Recover()` gracefully handle corrupted entries. Updated `TestWALCRCError` to verify graceful recovery. | 22.06.2026 | ✅ CLOSED |
| TD-GS-01 | Graceful shutdown for VLog | Added `VLogImpl.Shutdown(timeout)`, `LSMEngine.Shutdown()`, and 4 tests (`TestVLogShutdownGraceful`, `TestVLogShutdownTimeout`, `TestVLogShutdownNoViews`, `TestVLogShutdownRejectsNewViews`). VLog waits for active views with configurable timeout. | 22.06.2026 | ✅ CLOSED |
| TD-ZC-01 | Zero‑copy Value Log | Implemented VLogView with reference counting. Integrated into mergeIterator. Scan uses zero-copy reads from mmap. All tests pass with -race. | 22.06.2026 | ✅ CLOSED |
| TD-QW3-01 | Bloom filter using rand/fnv with allocations | Replaced `rand` with `fastrand` (lock-free) and inlined FNV-1a hash. Removed `hash/fnv` dependency. Bloom filter now has **0 allocs/op**, ~14.8 µs per op. | 22.06.2026 | ✅ CLOSED |
| TD-QW4-01 | WAL encode buffer allocations | Added `sync.Pool` for encode buffers in `encodeWalEntry`. Added `encodeBufferPool`, `getEncodeBuffer()`, `putEncodeBuffer()`. Buffer is zeroed before return to pool to prevent data leakage. | 22.06.2026 | ✅ CLOSED |
| TD-QW5-01 | `sync.Map` → `map[string]uint64` + `sync.RWMutex` | Replaced `sync.Map` with `map[string]uint64` + `sync.RWMutex` in `lastCommitCache`. `sync.Map` is optimized for read-heavy with rare writes, but `lastCommitCache` has few keys with frequent writes. `map+RWMutex` is simpler and faster for this pattern. Changed `LSMEngine.lastCommitCache` type, updated `updateLastCommitCache`/`getLastCommitCache` in `cache.go`, initialized map in `NewLSMEngine`. | 22.06.2026 | ✅ CLOSED |
---

## 📊 Priority Summary

| Priority | Count | Target |
|----------|-------|--------|
| 🔴 Critical | 15 | v0.3.0 – v0.4.0 |
| 🟡 Important | 9 | v0.4.0 |
| ✅ Closed | 11 | Done |

---

*This file tracks technical debt that must be fixed for production readiness.*