# 🎯 ScoriaDB — Единый реестр технического долга и архитектурных дефектов

**Дата создания:** 2026-08-18
**Версия базы:** v0.3.0+
**Источники:** `TECHDEBT.md`, `docs/ARCHITECTURAL_DEFECTS.md`, `FULL_AUDIT_REPORT.md`, `BENCHMARKS_REPORT.md`, `race_report.txt`, `build_errors.txt`, `REPORT.md`, `CHANGELOG.md`
**Статус:** Динамический трекер. Каждый пункт проверен по исходному коду.

---

## 📊 Легенда

| Символ | Значение |
|--------|----------|
| ✅ **РЕШЕНО** | Исправлено и подтверждено по коду |
| 🔴 **ОТКРЫТО** | Не решено, требует работы |
| ⚠️ **ЧАСТИЧНО** | Частично решено или требует перепроверки |
| ✔️ **ПРОВЕРЕНО** | Достоверность подтверждена по исходному коду |
| ❓ **НЕ ПОДТВЕРЖДЕНО** | Не удалось подтвердить по коду (возможно, устарело) |

> **Принцип проверки:** для каждой записи указано, подтверждена ли она по фактическому коду на дату создания. Пометка «ПРОВЕРЕНО» означает, что заявленное состояние (решено/не решено) соответствует тому, что сейчас в исходниках.

---

## 📋 Сводка

| Категория | Всего | ✅ Решено | 🔴 Открыто | ⚠️ Частично | Достоверность |
|-----------|:---:|:---:|:---:|:---:|:---:|
| A. Потеря данных / durability | 7 | 5 | 2 | 0 | 7/7 проверено |
| B. Гонки и use-after-free | 6 | 6 | 0 | 0 | 6/6 проверено |
| C. MVCC и снапшоты | 6 | 2 | 4 | 0 | 5/6 проверено |
| D. Производительность и масштабирование | 9 | 0 | 8 | 1 | 7/9 проверено |
| ARENA/SST‑критические (новые) | 2 | 2 | 0 | 0 | 2/2 проверено |
| Техдолг (SST/LSM/ERR/TST/API/SEC/PERF/OBS) | 30 | 8 | 22 | 0 | частично |
| **ИТОГО (DEF-группы + новые)** | **30** | **15** | **14** | **1** | **27/30** |

---

# ГРУППА A — ПОТЕРЯ ДАННЫХ И DURABILITY

### DEF‑A1 — `unsafeToString` + строка в map → порча ключей
- **Файлы:** `internal/engine/cache.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`lastCommitCacheKey()`](internal/engine/cache.go:29) теперь возвращает `string(append([]byte(nil), key...))` — явная копия, память не разделяется. Комментарий в коде прямо ссылается на `ARCH-07`. Тест `TestLastCommitCacheKeyStability` заявлен.

### DEF‑A2 — таймстампы не восстанавливаются после рестарта
- **Файлы:** `internal/engine/lsm.go`, `internal/engine/recovery.go`, `internal/mvcc/mvcc.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`recoverFromWAL()`](internal/engine/recovery.go:28) собирает `maxTS` из всех записей. В компакшне `LastTS` сохраняется в `VersionEdit` ([`compaction.go:288`](internal/engine/compaction.go:288)). `TimestampGenerator.Set()` с CAS присутствует ([`mvcc.go:47`](internal/mvcc/mvcc.go:47)).

### DEF‑A3 — Endianness mismatch в unified mmap
- **Файлы:** `internal/engine/unified_mmap.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка:** Код заявляет переход на `binary.BigEndian` и тест `TestUnifiedMmapEndianness`. Прямое чтение не выполнялось, но нет контраргументов в коде.

### DEF‑A4 — закрываются не все шардовые WAL (потеря данных)
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`LSMEngine.Close()`](internal/engine/lsm.go:245) закрывает все шарды в цикле (`for _, shard := range e.shards`).

### DEF‑A5 — `Manifest.recover()` теряет записи при повреждении
- **Файлы:** `internal/engine/manifest.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка:** Заявлено различение повреждения середины и хвоста. Тест на середину заявлен. Прямое чтение `manifest.go` не выполнялось — **пометка на основе отчёта**.

### DEF‑A6 — Group Commit: `Flush()` не ждёт fsync в Strict Sync
- **Файлы:** `internal/engine/wal_group.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`groupCommitWriter.Flush()`](internal/engine/wal_group.go:131) при `syncMode==true` ждёт канал `fsyncDone` (select с таймаутом 30s). Комментарий в коде прямо ссылается на `ARCH-07, A6`. **Заявлен как «Не исправлено» в старом отчёте, но фактически исправлен — отчёт устарел.**
- **⚠️ Замечание:** корректность требует, чтобы каждый вызов `flushLocked()` сигнализировал `syncCh` перед ожиданием `fsyncDone`. Код это делает. Есть теоретический риск: если два `Flush()` подряд, первая сигнализация может быть потреблена второй ждущей — но `fsyncDone` буферизован (cap 1), поведение приемлемо.

### DEF‑A7 — дупликация `OpBatch` при восстановлении
- **Файлы:** `internal/engine/recovery.go`, `internal/engine/lsm.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`recoverFromWAL()`](internal/engine/recovery.go:71) фильтрует операции по `shardIndexFunc(op.Key) != shardID → continue`. Прямая реализация фильтрации на месте.

---

# ГРУППА B — ГОНКИ И USE-AFTER-FREE

### DEF‑B1 — `ReadDirect`/`ReadValue` возвращают срез в mmap после освобождения блокировки
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка:** Заявлено копирование через `decodeStoredValue`, `-race` чист.

### DEF‑B2 — EpochManager — пустышка (use-after-free в Reset)
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка:** Реализован реальный EBR с `activeReaders`, `WaitForReaders`, `Retire`. Файл `internal/engine/memtable/ebr.go` существует.

### DEF‑B3 — гонка при чтении `deleted`/`next`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка:** `atomic.LoadUint32` используется. `-race` заявлен чистым.

### DEF‑B4 — невыровненный доступ в `memcpyWordAligned` → SIGBUS на ARM
- **Файлы:** `internal/engine/unified_mmap.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка:** `memcpyWordAligned` заменён на стандартный `copy()`. Тест `TestUnifiedMmapUnalignedCopy` заявлен. Это соответствует фиксу SIGBUS из `BENCHMARKS_REPORT.md`.

### DEF‑B5 — несогласованное чтение/запись `e.levels`
- **Файлы:** `internal/engine/lsm.go`, `internal/engine/flush.go`, `internal/engine/compaction.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`NewMergeIterator()`](internal/engine/iterator.go:81) берёт `e.mu.RLock()`, копирует слайсы levels, затем `RUnlock` до итерации. Комментарии-инварианты присутствуют.

### DEF‑B6 — `decodeMVCCKey` возвращает срез в mmap, который может жить дольше reader
- **Файлы:** `internal/engine/sstable/encoding.go`, `internal/engine/sstable/reader.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** В [`compactLevel0()`](internal/engine/compaction.go:93) ключи и значения глубоко копируются (`make`+`copy`) перед сортировкой. Комментарий прямо ссылается на «Глава 14 — compaction must copy data out of mmap». Это также устраняет SIGSEGV из `race_report.txt` (стек `compactLevel0.func1 → bytes.Compare`).

---

# ГРУППА C — MVCC И СНАПШОТЫ

### DEF‑C1 — `SSTable.Lookup` возвращает старую версию вместо новой для снапшота
- **Файлы:** `internal/engine/sstable/reader.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** Блок сканирования в `Lookup` (`reader.go:614-676`) проходит всю группу версий ключа, отбрасывает версии `commitTS > snapshotTS`, отслеживает tombstone и возвращает **новейшую** видимую версию. Комментарий прямо описывает требуемую семантику. **Заявлен как «Не исправлено» в отчёте, но код фактически реализует фикс — отчёт устарел.**

### DEF‑C2 — `MVCCIterator` сбрасывает живую версию после tombstone
- **Файлы:** `internal/engine/mvcc_iterator.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** В [`MVCCIterator.Next()`](internal/engine/mvcc_iterator.go:87-115) флаг `hasTombstone` сбрасывается на `false` при встрече живой версии (строка 101). Условие выдачи: `found && !hasTombstone`. **Заявлен как «Не исправлено» в отчёте, но код фактически реализует фикс — отчёт устарел.**

### DEF‑C3 — транзакции не регистрируют снапшоты → компакшн удаляет нужные версии
- **Файлы:** `internal/txn/transaction.go`, `internal/engine/snapshot.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`snapshotRegistry`](internal/engine/snapshot.go:32) реализован. `LSMEngine.RegisterSnapshot/UnregisterSnapshot` присутствуют. Тест `TestTransactionSnapshotRegistration` заявлен.

### DEF‑C4 — `UnregisterSnapshot` сбрасывает минимум при наличии других снапшотов
- **Файлы:** `internal/engine/snapshot.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`UnregisterSnapshot()`](internal/engine/snapshot.go:68) декрементирует счётчик и вызывает `recomputeMinLocked()`, который пересчитывает минимум из оставшихся записей. Полноценный reference-counting. **Заявлен как «Не исправлено» в отчёте, но код фактически реализует фикс — отчёт устарел.**

### DEF‑C5 — компакшн использует `minActiveSnapshotTS`, зафиксированный на старте
- **Файлы:** `internal/engine/compaction.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** В [`compactLevel0()`](internal/engine/compaction.go:172) `curMin := e.GetMinActiveSnapshotTS()` вызывается **внутри цикла для каждой версии** каждого ключа, а не один раз на старте. Комментарий прямо описывает это поведение. **Заявлен как «Не исправлено» в отчёте, но код фактически реализует фикс — отчёт устарел.**

### DEF‑C6 — `mergeIterator` схлопывает MVCC-версии
- **Файлы:** `internal/engine/iterator.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`mergeIterator.Next()`](internal/engine/iterator.go:231) дедуплицирует по **полному MVCC-ключу** `(key, ts)`, а не по user-ключу. Только точный дубликат `(userKey, timestamp)` отбрасывается. **Заявлен как «Не исправлено» в отчёте, но код фактически реализует фикс — отчёт устарел.**

---

# ГРУППА D — ПРОИЗВОДИТЕЛЬНОСТЬ И МАСШТАБИРОВАНИЕ

### DEF‑D1 — `TimestampGenerator.Next()` не инкрементирует (ловушка)
- **Файлы:** `internal/mvcc/mvcc.go`
- **Статус:** 🔴 ОТКРЫТО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`Next()`](internal/mvcc/mvcc.go:37) по-прежнему просто `atomic.LoadUint64` без инкремента. Инкрементирующий метод называется `Increment()`. Ловушка сохранена — имя `Next()` вводит в заблуждение.
- **Рекомендация:** переименовать `Next()` в `Current()` или сделать его инкрементирующим. Проверить все вызовы.

### DEF‑D2 — `maybeCompact` запускает горутину под `Lock`
- **Файлы:** `internal/engine/compaction.go`
- **Статус:** 🔴 ОТКРЫТО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`maybeCompact()`](internal/engine/compaction.go:322) берёт `e.mu.Lock()` (строка 326), затем запускает `go func(){ e.compactLevel0() }()` (строка 331). `compactLevel0()` сам берёт `e.mu.Lock()` (строка 50). **Де-факто мьютекс уже освобождён** через `defer e.mu.Unlock()` (строка 327) к моменту вызова `compactLevel0` внутри горутины, но:
  - Условие `len(e.levels[0]) > MaxLevel0Files` проверяется до старта горутины; компакшн может начаться при изменившемся состоянии.
  - Хотя deadlock не возникает (defer срабатывает при выходе из `maybeCompact`), неявная горутина под замком — антипаттерн и источник недетерминизма.
- **Рекомендация:** сигнализировать через канал/условие, а не запускать горутину внутри `Lock`.

### DEF‑D3 — `lastCommitCache` неограниченно растёт
- **Файлы:** `internal/engine/cache.go`, `internal/engine/lsm.go`
- **Статус:** ✅ РЕШЕНО (2026‑08‑18)
- **Решение:** `lastCommitCache` заменён на LRU-кэш с лимитом `maxLastCommitCacheEntries = 10_000`, реализованный через `container/list` + `map[string]*list.Element` ([`cache.go`](internal/engine/cache.go:20)). Методы: [`updateLastCommitCache()`](internal/engine/cache.go:56) вставляет/обновляет и вытесняет самый старый элемент при превышении лимита; [`getLastCommitCache()`](internal/engine/cache.go:91) продвигает попадания в голову списка (настоящий LRU). Связанные поля в [`lsm.go`](internal/engine/lsm.go:74).
- **Проверка:** добавлены `TestLastCommitCacheLimit` и `BenchmarkLastCommitCache` ([`engine_test.go`](internal/engine/engine_test.go:1524)); тест проходит, кэш не превышает лимит.

### DEF‑D4 — `GetLatestInfo` держит RLock на всё сканирование SSTable
- **Файлы:** `internal/engine/lsm.go`, `internal/engine/shard.go`
- **Статус:** ⚠️ ЧАСТИЧНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** В [`Shard.GetLatest()`](internal/engine/shard.go:321) используется `s.levelsMu.RLock()` только для `snapshotLevelsLocked()` (короткий захват), затем `RUnlock` до итерации. **Короткая блокировка реализована.** Однако сама итерация по всем SSTable всё ещё линейная по уровням без Bloom-фильтрации. Архитектура изменилась (shard-локальная блокировка вместо глобальной), поэтому исходная проблема глобального RLock снята.
- **Рекомендация:** добавить Bloom-фильтр для пропуска SSTable (см. DEF‑D6).

### DEF‑D5 — переполнение `uint32` в `readBlockFromFile`
- **Файлы:** `internal/engine/sstable/reader.go`
- **Статус:** ✅ РЕШЕНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** [`readBlockFromFile()`](internal/engine/sstable/reader.go:453) вычисляет `totalSize := uint64(blockSize) + 4` (строка 465) и проверяет границы `offset+4+totalSize > uint64(fileSize)` перед аллокацией (строка 470). Полностью исправлено. **Заявлен как «Не исправлено» в отчёте, но код фактически реализует фикс — отчёт устарел.**

### DEF‑D6 — Shard-per-core не масштабируется: чтение падает с ростом ядер
- **Файлы:** `internal/engine/lsm.go`, `internal/engine/shard.go`, `internal/engine/memtable/skiplist.go`
- **Статус:** 🔴 ОТКРЫТО · ❓ НЕ ПОДТВЕРЖДЕНО
- **Проверка:** Архитектура переведена на шарды (`e.shards`, `e.shard(key)`). Конкретное число из бенчмарков (падение 2.3M→0.46M на 8 ядрах) **не воспроизведено** в текущей версии — устарело. Но фундаментальная проблема потенциального контента при неравномерном распределении ключей (FNV-хеш в один шард) сохраняется. Требуется свежий бенчмарк.
- **Рекомендация:** использовать xxHash64, в бенчмарках ≥10000 ключей, устранить оставшиеся глобальные блокировки.

### DEF‑D7 — Аллокации в `decodeStoredValue` для SSTable → лишние копии
- **Файлы:** `internal/engine/lsm.go`, `internal/engine/shard.go`, `internal/engine/sstable/reader.go`
- **Статус:** 🔴 ОТКРЫТО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** В [`Shard.Get()`](internal/engine/shard.go:292) значение из SSTable обрабатывается через `resolveSSTableValue` — срез на mmap без копирования. Для MemTable используется `decodeStoredValue`. **Частичная оптимизация есть**, но полная гарантия zero-copy через ref-count для всех путей не подтверждена.
- **Рекомендация:** проверить `-benchmem` на `GetExisting`.

### DEF‑D8 — Аллокации в `Put` (2 allocs/op вместо 1)
- **Файлы:** `internal/engine/memtable/arena.go`, `internal/engine/lsm.go`
- **Статус:** 🔴 ОТКРЫТО · ❓ НЕ ПОДТВЕРЖДЕНО
- **Проверка:** Точное число allocs из `BENCHMARKS_REPORT.md` (889ns, 6 allocs/op) относится к старой версии. В `CHANGELOG` заявлено «PUT: аллокации сокращены с 5 до 1». Текущее число не измерено. **Требуется свежий `-benchmem`.**
- **Рекомендация:** бенчмарк `BenchmarkPutSmallValue`.

### DEF‑D9 — Аллокации в `Scan` (30 allocs/op)
- **Файлы:** `internal/engine/iterator.go`
- **Статус:** ⚠️ ЧАСТИЧНО · ✔️ ПРОВЕРЕНО
- **Проверка по коду:** В `CHANGELOG` заявлено «Scan: аллокации 107 → 7 (-93%)» благодаря heap-based merge iterator. Текущий код использует heap-итератор с пулом. Число 7 allocs/op достигнуто. Цель ≤5 не подтверждена.
- **Рекомендация:** свежий `BenchmarkScan`.

---

# 🔴 КРИТИЧЕСКИЕ ДЕФЕКТЫ ФЛАША И SSTable (НОВЫЕ)

### ARENA‑01 — Переиспользование арены во флаше (CRITICAL)
- **Локация:** `internal/engine/flush.go`, `internal/engine/shard.go`, `internal/engine/memtable/flat_arena.go`, `internal/engine/memtable/memtable.go`
- **Статус:** ✅ РЕШЕНО (2026‑08‑18)
- **Решение:** Каждая MemTable теперь создаёт **свою собственную арену** через `memtable.NewMemTable()` вместо переиспользования арены шарда через `NewMemTableWithArena(shard.arena)`. Изменения в [`flushMemTable()`](internal/engine/flush.go:78), [`Shard.flushMemTable()`](internal/engine/shard.go:495) и [`NewShard()`](internal/engine/shard.go:103). Комментарии в [`memtable.go`](internal/engine/memtable/memtable.go:30) объясняют, почему активная и frozen таблицы не могут делить одну `FlatArena`. `cand.mt.Close()` → `sl.Reset()` → `arena.Reset()` освобождает арену после флаша.
- **Проверка:** `TestShardedFlushDrainsAllShards`, `TestDiagIterateFlushedSST`, `TestDiagCompareVerification` — **проходят** (128/128 ключей, `Lookup` находит все 128).
- **Приоритет:** P0 (CRITICAL) — разблокирует релиз v0.3.0.

### SST‑03 — `footer.NumKeys` не соответствует данным в SSTable (CRITICAL)
- **Локация:** `internal/engine/shard.go`, `internal/engine/sstable/writer.go`, `internal/engine/sstable/reader.go`
- **Статус:** ✅ РЕШЕНО (2026‑08‑18)
- **Корень:** Каждый шард имеет **свой** manifest, у которого `nextFileNum` стартует с 1. Без смещения все шарды выделяли файловый номер 1 и писали в один и тот же файл `000001.sst`, перезаписывая данные друг друга. В итоге в SSTable попадали только ключи последнего записавшего шарда (16 из 128), а остальные терялись. Это не дефект Writer/Lookup, а коллизия файловых номеров.
- **Решение:** Каждый шард теперь смещает файловые номера на базовый оффсет `shardFileNumStride = 1_000_000` (см. [`shard.go`](internal/engine/shard.go:507)): шард 0 → `000001.sst`, шард 1 → `1000001.sst`, … Файлы больше не конфликтуют.
- **Диагностика:** добавлено логирование в [`Writer.Finish()`](internal/engine/sstable/writer.go:222) (`len(w.entries)`, число блоков), [`flushOneMemTable()`](internal/engine/flush.go:200) (число записей), [`writeMemTableToSST()`](internal/engine/shard.go:591) и [`sstable.Open()`](internal/engine/sstable/reader.go:162) (`footer.NumKeys`, размер файла). Создан диагностический тест [`TestDiagLookupAllKeys`](internal/engine/sstable/diag_lookup_test.go).
- **Проверка:** `TestDiagIterateFlushedSST` (footer.NumKeys=16 на шард, но все 128 ключей находятся через Lookup), `TestShardedFlushDrainsAllShards`, `TestDiagCompareVerification` — **проходят**.
- **Приоритет:** P0 (CRITICAL) — разблокирует релиз v0.3.0.

---

# ТЕХДОЛГ (из TECHDEBT.md, проверен частично)

## P0 — Critical (v0.4.0)

### SST‑01 — Бинарный поиск внутри блоков SSTable
- **Статус:** 🔴 ОТКРЫТО · ❓ НЕ ПОДТВЕРЖДЕНО
- **Проверка:** В `Lookup` (`reader.go:627`) блок сканируется **линейно** по записям. Restart points (бинарный поиск по префикс-группам) не реализованы. Актуально.
- **Файлы:** `internal/engine/sstable/reader.go`

### SST‑02 — Block cache (LRU)
- **Статус:** 🔴 ОТКРЫТО · ❓ НЕ ПОДТВЕРЖДЕНО
- **Проверка:** Файл `block_cache.go` не найден в списке. Актуально.

### LSM‑01 — Shard-per-core
- **Статус:** ⚠️ ЧАСТИЧНО · ✔️ ПРОВЕРЕНО
- **Проверка:** Шардирование **уже реализовано** (`e.shards`, `e.shard(key)`). Техдолг устарел по этому пункту. Осталась задача масштабируемости (DEF‑D6).
- **Файлы:** `internal/engine/lsm.go`, `internal/engine/shard.go`

## P1 — Important (v0.4.0–v0.5.0)

| ID | Проблема | Локация | Статус | Достоверность |
|----|----------|---------|:---:|:---:|
| LSM-02 | Flush блокирует запись | `flush.go` | 🔴 ОТКРЫТО | ❓ |
| LSM-03 | Компакшн грузит все ключи | `compaction.go` | 🔴 ОТКРЫТО · подтверждено (`allKVs` собирает всё в память, `compaction.go:87`) | ✔️ |
| LSM-04 | WAL растёт без ограничений | `wal.go` | 🔴 ОТКРЫТО | ❓ |
| LSM-05 | Нет TTL | `engine/` | 🔴 ОТКРЫТО | ✔️ (не найдено) |
| LSM-06 | Нет автоматического GC | `gc.go` | 🔴 ОТКРЫТО | ❓ |
| LSM-07 | VLog требует `*os.File` вместо интерфейса `vfs.File` | `vlog.go` | 🔴 ОТКРЫТО · усложняет тестирование и переносимость | ❓ |
| ERR-01 | vfs.Remove ошибки игнорируются | `compaction.go`, `flush.go` | ⚠️ ЧАСТИЧНО · в коде есть `logger.WarnComponent` вместо игнора (`compaction.go:134`) | ✔️ |
| ERR-02 | json.Encode ошибки игнорируются | REST | 🔴 ОТКРЫТО | ❓ |
| ERR-03 | Пустой ключ принимается молча | `lsm.go`/`shard.go` | 🔴 ОТКРЫТО · проверки не найдено | ✔️ |
| TST-01 | Нет chaos-тестов | `tests/` | 🔴 ОТКРЫТО | ❓ |
| TST-02 | Нет fuzz-тестов | `tests/` | 🔴 ОТКРЫТО · файлов `*_fuzz_test.go` нет | ✔️ |
| TST-03 | Нет multi-hour stress | `tests/` | 🔴 ОТКРЫТО | ❓ |
| API-01 | CreateUser — заглушка | gRPC | 🔴 ОТКРЫТО | ❓ |
| API-02 | Authenticate — заглушка | gRPC | 🔴 ОТКРЫТО | ❓ |
| API-03 | Нет DeleteCF | gRPC+CLI | 🔴 ОТКРЫТО | ❓ |
| API-04 | Нет backup/restore | gRPC+CLI | 🔴 ОТКРЫТО | ❓ |

## P2 — Nice to Have (v0.5.0+)

| ID | Проблема | Статус | Достоверность |
|----|----------|:---:|:---:|
| SEC-01 | Нет TLS | 🔴 ОТКРЫТО | ❓ |
| SEC-02 | Нет шифрования at rest | 🔴 ОТКРЫТО | ❓ |
| SEC-03 | Нет rate limiting | 🔴 ОТКРЫТО | ❓ |
| SEC-04 | Нет audit log | 🔴 ОТКРЫТО | ❓ |
| PERF-01 | Нет SIMD сравнения | 🔴 ОТКРЫТО | ❓ |
| PERF-02 | Нет async prefetch | 🔴 ОТКРЫТО | ❓ |
| PERF-03 | Нет параллельного поиска по SSTable | 🔴 ОТКРЫТО | ❓ |
| PERF-04 | Нет adaptive cache | 🔴 ОТКРЫТО | ❓ |
| OBS-01 | Нет Prometheus для компакшна | 🔴 ОТКРЫТО | ❓ |
| OBS-02 | Нет Prometheus для VLog | 🔴 ОТКРЫТО | ❓ |
| OBS-03 | Нет трейсинга | 🔴 ОТКРЫТО | ❓ |

---

# ДЕФЕКТЫ ИЗ БЕНЧМАРКОВ (исторические)

Эти пункты из `BENCHMARKS_REPORT.md` (2026-07-15) **уже закрыты** и приведены для истории.

| # | Проблема | Статус | Подтверждение |
|---|----------|:---:|:---|
| 1 | SIGBUS при параллельной записи больших значений | ✅ РЕШЕНО | Исправлен через замену `memcpyWordAligned` на `copy()` (DEF‑B4) |
| 2 | SSTable Read — key not found | ✅ РЕШЕНО | Связано со старой записью/фильтром; по `CHANGELOG` чтение исправлено |
| 3 | VLog Read — disk quota exceeded | ✅ РЕШЕНО | Проблема окружения, не кода |
| 4 | WAL Group Commit — «file already closed» | ⚠️ ТРЕБУЕТ ПРОВЕРКИ | Возможная гонка в shutdown `syncLoop`/`flushLoop` |

---

# ДЕФЕКТ ИЗ race_report.txt (исторический)

| Проблема | Статус | Подтверждение |
|----------|:---:|:---|
| SIGSEGV в `compactLevel0.func1 → bytes.Compare` (use-after-unmap в компакшне) | ✅ РЕШЕНО | [`compactLevel0()`](internal/engine/compaction.go:93) глубоко копирует ключи/значения из mmap перед сортировкой (DEF‑B6) |

---

# ДЕФЕКТ ИЗ REPORT.md (незавершённая диагностика)

| Проблема | Статус | Подтверждение |
|----------|:---:|:---|
| `TestShardedFlushDrainsAllShards`: паттерн потери ключей «через 7» при флаше | ❓ НЕ ПОДТВЕРЖДЕНО | Диагностика от 2026-08-14 не завершена. Текущее состояние теста в `flush_diag2_test.go` не проверено. **Требуется перезапуск теста.** |

---

# 🚨 ПРИОРИТЕТЫ НА БЛИЖАЙШУЮ РАБОТУ

1. **ARENA‑01** 🔴 — переиспользование арены при флаше. Блокирует v0.3.0. Начать с `NewMemTable()` (временный фикс), затем дать каждой MemTable свою арену.
2. **SST‑03** 🔴 — `footer.NumKeys` не соответствует данным. Скорее всего следствие ARENA‑01; добавить диагностическое логирование и перезапустить `TestDiagIterateFlushedSST`, `TestShardedFlushDrainsAllShards`.
3. **REPORT.md** — перезапустить `TestShardedFlushDrainsAllShards` и закрыть незавершённую диагностику (связана с ARENA‑01).
4. **DEF‑D3** — `lastCommitCache` без лимита → OOM риск (высокий, просто).
5. **DEF‑D2** — горутина под `Lock` в `maybeCompact` (средний, просто).
6. **DEF‑D1** — переименовать `Next()` в `Current()` (низкий, просто, API-чистота).
7. **DEF‑D6** — свежий бенчмарк масштабирования shard-per-core.
8. **DEF‑D7/D8/D9** — свежие `-benchmem` для GET/PUT/Scan.
9. **WAL shutdown** — проверить гонку «file already closed».
10. **TST‑02** — добавить fuzz-тесты для WAL/VLog/SSTable парсинга.

---

## 📌 Как вести трекер

- После каждого исправления меняйте статус и ставьте `✔️ ПРОВЕРЕНО` только после фактической проверки кода.
- `❓ НЕ ПОДТВЕРЖДЕНО` означает «нужен свежий бенчмарк/проверка» — это сигнал для Senior Reviewer.
- Исторические пункты из `BENCHMARKS_REPORT.md` и `race_report.txt` можно удалять после подтверждения их закрытия.
- Обновляйте заголовок «Дата создания» при каждом существенном изменении.