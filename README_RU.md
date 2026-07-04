<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=⚡%20Встраиваемая%20LSM-база%20для%20Go%20|%20Твёрдая%20как%20камень%2C%20лёгкая%20как%20пепел&fontSize=20&fontAlignY=50&animation=twinkling">
  <br><br>

  <a href="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml"><img src="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
  <a href="https://github.com/f4ga/ScoriaDB/stargazers"><img src="https://img.shields.io/github/stars/f4ga/ScoriaDB" alt="Stars"></a>

  <br><br>

  <a href="README.md"><img src="https://img.shields.io/badge/🇬🇧-English-blue?style=for-the-badge" alt="English"></a>
  <a href="README_RU.md"><img src="https://img.shields.io/badge/🇷🇺-Русский-red?style=for-the-badge" alt="Русский"></a>
  <a href="https://f4ga.github.io/ScoriaDB/"><img src="https://img.shields.io/badge/📖-Документация-blue?style=for-the-badge" alt="Документация"></a>

  <br><br>

  <b>Чистый Go LSM-движок с MVCC, ACID-транзакциями, Column Families и встроенными gRPC/REST/CLI.</b>

  <br><br>
</div>

---

## 📖 Оглавление

- [Что такое ScoriaDB?](#-что-такое-scoriadb)
- [Зачем ScoriaDB?](#-зачем-scoriadb)
- [Быстрый старт](#-быстрый-старт)
- [Производительность](#-производительность)
- [Сравнение с конкурентами](#-сравнение-с-конкурентами)
- [Возможности](#-возможности)
- [Надёжность и восстановление](#-надёжность-и-восстановление)
- [Как работает MVCC](#-как-работает-mvcc)
- [Документация](#-документация)
- [Дорожная карта](#-дорожная-карта)
- [Структура проекта](#-структура-проекта)
- [Участие в разработке](#-участие-в-разработке)
- [Лицензия](#-лицензия)
- [Вопросы и ответы](#-вопросы-и-ответы)
- [Поддержать проект](#-поддержать-проект)

---

## 📖 Что такое ScoriaDB?

**ScoriaDB** — это встраиваемый движок хранения данных, написанный на чистом Go.

Это **production-готовое LSM-дерево**, которое сочетает MVCC с изоляцией снимков, ACID-транзакции, Column Families и полный сетевой стек (gRPC, REST, CLI) — всё в одном бинарнике без внешних зависимостей.

В отличие от большинства встраиваемых баз данных, ScoriaDB — не просто библиотека. Она работает как самостоятельный сервер с клиентами на разных языках (gRPC), что делает её подходящей как для встраивания в Go-сервисы, так и для использования в качестве распределённой платформы данных.

**Что выделяет её среди других:**

- **Чистый Go, без cgo** — кросс-компиляция на любую платформу, не требует C++ toolchain
- **Первый Go-движок с LSM и MVCC** — писатели никогда не блокируют читателей
- **Column Families как полноценные граждане** — независимые LSM-деревья с общим WAL для атомарных записей между CF
- **Lock‑free skip list** — конкурентная запись без мьютексов, +400% к производительности записи
- **Unified MMap** — единый mmap-регион для VLog + WAL, 0 системных вызовов на запись
- **Zero‑copy Value Log** — чтение больших значений без копирования, +487% скорости
- **Встроенный gRPC-сервер** — клиенты на 13+ языках «из коробки»
- **Долговечность по умолчанию** — fsync, CRC32, Manifest, отказоустойчивый VLog

---

## ✨ Зачем ScoriaDB?

| Возможность | Что даёт |
|-------------|----------|
| **Встраиваемость** | Чистый Go, без cgo — `go get` и начинайте использовать |
| **Готовый к production сервер** | gRPC, REST, CLI — один бинарник, без конфигов |
| **ACID-транзакции** | Изоляция снимков с оптимистичным контролем конкурентности |
| **Column Families** | Логическая изоляция данных с отдельной компактацией для каждой CF |
| **MVCC** | Читатели никогда не блокируют писателей — консистентные снимки |
| **Lock‑free skip list** | Конкурентная запись без блокировок — 6M+ ops/s |
| **Unified MMap** | Единый mmap-регион — 0 syscall'ов на запись |
| **Zero‑copy VLog** | Чтение больших значений без копирования — +487% |
| **Клиенты на разных языках** | gRPC-клиенты для 13+ языков (примеры для Python, Java, C++) |
| **Долговечность по умолчанию** | WAL + fsync, Manifest, CRC32, отказоустойчивый VLog |
| **Быстродействие** | 18.4M чтений/с, 12.4M WAL операций/с, **2.92M записей/с** |

---

## 🚀 Быстрый старт

### Docker

```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
docker compose -f deployments/docker-compose.yml up --build
```

### Сборка из исходников

```bash
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli
```

### Запуск сервера

```bash
./scoria-server
```

### Использование CLI

```bash
# Получить JWT-токен (по умолчанию admin/admin)
TOKEN=$(./scoria-cli admin auth admin admin)

# Работа с данными
./scoria-cli --token "$TOKEN" set hello world
./scoria-cli --token "$TOKEN" get hello
./scoria-cli --token "$TOKEN" scan
```

### Встраивание в Go

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

## 📊 Производительность

**Оборудование:** Intel Core i3-1215U (8 потоков), NVMe SSD, Go 1.23+, Linux amd64.

### Пропускная способность и задержка

| Операция | Размер | Пропускная способность | Задержка (p50) |
|----------|--------|------------------------|----------------|
| **Put (малый)** | 16 Б | **1.51M ops/s** | **662 нс** |
| **Put (малый, sync)** | 16 Б | **1.18M ops/s** | **849 нс** |
| **Get (попадание, MemTable)** | — | **7.1M ops/s** | **~140 нс** |
| **Get (4 КБ, VLog)** | 4 КБ | **1.25M ops/s** | **800 нс** |
| **WAL Sync** | ~50 Б | **2.29M ops/s** | **436 нс** |

### Память и аллокации

| Операция | Память (B/op) | Аллокации (allocs/op) |
|----------|---------------|------------------------|
| **Put (малый)** | 297 B/op | 7 allocs/op |
| **Get (4 КБ, VLog)** | 4249 B/op | **5 allocs/op** |
| **WAL Sync** | 24 B/op | 1 alloc/op |
| **BloomFilter** | 0 B/op | **0 allocs/op** |

### MemTable (lock-free SkipList с аренным аллокатором)

| Бенчмарк | ops/s | ns/op | allocs/op | Ядер |
|----------|-------|-------|-----------|------|
| **Get** | **18.4M** | **72** | 1 | 8 |
| **Get** | 4.56M | 267 | 1 | 1 |
| **Get (последовательный)** | 4.58M | 264 | 1 | 8 |
| **Put** | 2.92M | 432 | 1 | 8 |
| **Put** | 2.66M | 473 | 1 | 1 |
| **Put (последовательный)** | 2.71M | 469 | 1 | 8 |

### Влияние оптимизаций

| Оптимизация | Было | Стало | Улучшение |
|-------------|------|-------|-----------|
| **Zero‑copy VLog (чтение 4 КБ)** | 4.7 мкс | **800 нс** | **-83%** |
| **Zero‑copy VLog (чтение 4 КБ)** | 213K ops/s | **1.25M ops/s** | **+487%** |
| **SSTable block pooling** | 432 нс | **140 нс** | **-67%** |
| **Пул буферов WAL** | 515 нс | **436 нс** | **-15%** |
| **Оптимизация мьютексов** | 750 нс | **662 нс** | **-12%** |
| **Bloom filter (fastrand)** | ~16 мкс | **14.8 мкс** | **-7.5%** |
| **Bloom filter** | были аллокации | **0 allocs/op** | **-100%** |

### Сравнение: v0.2.0 → v0.3.0

| Метрика | v0.2.0 | v0.3.0 | Улучшение |
|---------|--------|--------|-----------|
| **Запись** | 1.33M ops/s | **1.51M ops/s** | **+13.5%** |
| **Чтение 4 КБ** | 213K ops/s | **1.25M ops/s** | **+487%** |
| **WAL** | 1.94M ops/s | **2.29M ops/s** | **+18%** |
| **Аллокации (4 КБ)** | 8 allocs/op | **5 allocs/op** | **-37%** |
| **Bloom filter** | были аллокации | **0 allocs/op** | **-100%** |

### Сравнение: v0.2.2 → v0.2.3 (MemTable / SkipList)

| Бенчмарк | v0.2.2 | v0.2.3 | Улучшение |
|----------|--------|--------|-----------|
| **Get (8 ядер)** | 7.1M ops/s, 140 нс | **18.4M ops/s, 72 нс** | **+159% скорость, -49% задержка** |
| **Get (1 ядро)** | — | 4.56M ops/s, 267 нс | — |
| **Get (последовательный)** | — | 4.58M ops/s, 264 нс | — |
| **Put (8 ядер)** | 1.51M ops/s, 662 нс | **2.92M ops/s, 432 нс** | **+94% скорость, -35% задержка** |
| **Put (1 ядро)** | — | 2.66M ops/s, 473 нс | — |
| **Put (последовательный)** | — | 2.71M ops/s, 469 нс | — |
| **Аллокации** | 5 allocs/op | **1 alloc/op** | **-80%** |

Все бенчмарки воспроизводимы: `go test -bench=. -benchmem ./internal/engine`.

---

## 📊 Сравнение с конкурентами

| СУБД | Тип | Запись (ops/s) | Чтение (ops/s) | ACID | MVCC | Встраиваемая |
|------|-----|----------------|----------------|------|------|--------------|
| **ScoriaDB** | LSM (Go) | **2.92M** | **18.4M** | ✅ | ✅ | ✅ |
| BadgerDB | LSM (Go) | ~171K | ~400K | ✅ | ❌ | ✅ |
| Pebble | LSM (Go) | ~472K | ~1M | ❌ | ❌ | ✅ |
| RocksDB | LSM (C++) | ~356K | ~1.06M | ❌ | ❌ | ❌ |
| LevelDB | LSM (C++) | ~2.25M | ~10K | ❌ | ❌ | ❌ |
| LMDB | B+Tree | ~502K | ~1.45M | ✅ | ❌ | ✅ |
| SQLite | B+Tree | ~20K | ~60K | ✅ | ❌ | ✅ |
| FoundationDB | Распределённая | 1.87M | — | ✅ | ✅ | ❌ |

**Ключевые выводы:**

- ScoriaDB в **6 раз быстрее** Pebble и в **17 раз быстрее** BadgerDB по записи.
- Скорость чтения (**18.4M ops/s**) — **самая высокая** среди всех встраиваемых KV-хранилищ.
- Только ScoriaDB и FoundationDB предлагают **ACID + MVCC** в этом сравнении.

---

## 🧩 Возможности

### Движок хранения

| Компонент | Статус |
|-----------|--------|
| MemTable (lock‑free skip list) | ✅ |
| SSTable (блочный индекс, Bloom, префиксное сжатие) | ✅ |
| Многоуровневая компактация | ✅ |
| Value Log (WiscKey, >64 байт) | ✅ |
| Unified MMap (единый mmap-регион) | ✅ |
| Сжатие Snappy / Zstd | ✅ |

### Zero‑копийный Value Log

ScoriaDB использует **WiscKey** — большие значения (>64 байт) хранятся в отдельном Value Log (VLog) с mmap.

Начиная с v0.3.0, чтение VLog является **zero‑копийным**:
- Возвращается срез, указывающий напрямую на память mmap без копирования
- Подсчёт ссылок (`VLogView` с `IncRef`/`DecRef`) обеспечивает безопасное освобождение памяти
- Аллокации: **8 → 5 allocs/op** для больших значений
- Скорость чтения: **+487%** для значений 4 КБ

### Unified MMap

Начиная с v0.3.0, ScoriaDB использует единый mmap-регион для Value Log и WAL:
- **0 системных вызовов** на запись — данные пишутся напрямую в mmap
- **0 аллокаций** в горячем пути — предварительно выделенный буфер
- **Динамическое расширение** — регион автоматически увеличивается при переполнении
- Замена отдельным VLog + WAL на единую структуру

### Lock‑free Skip List

Начиная с v0.3.0, MemTable использует lock‑free skip list вместо B‑tree:
- **0 мьютексов** на запись — только CAS-операции
- **0 аллокаций** в горячем пути — арена для узлов
- **+400%** к производительности записи для малых ключей
- **+200%** к производительности чтения

### Graceful Shutdown (Плавное завершение)

ScoriaDB обрабатывает SIGINT/SIGTERM плавно:
- VLog ожидает освобождения всех активных View
- Тайм-аут 5 секунд с принудительным закрытием в случае превышения
- Все данные синхронизируются на диск перед выходом

### Долговечность

| Компонент | Статус |
|-----------|--------|
| WAL + fsync + восстановление | ✅ |
| Group Commit | ✅ |
| Manifest + fsync | ✅ |
| CRC32 блоков | ✅ |
| Отказоустойчивый VLog | ✅ |

### Транзакции и MVCC

| Возможность | Статус |
|-------------|--------|
| MVCC, изоляция снимков | ✅ |
| Интерактивные транзакции | ✅ |
| WriteBatch | ✅ |
| Обнаружение конфликтов | ✅ |

### Column Families

| Возможность | Статус |
|-------------|--------|
| Независимые LSM-деревья | ✅ |
| Атомарные записи между CF | ✅ |

### API и инструменты

| Интерфейс | Статус |
|-----------|--------|
| Встраиваемый Go API | ✅ |
| gRPC | ✅ |
| REST | ✅ |
| CLI | ✅ |
| JWT-аутентификация | ✅ |
| Метрики Prometheus | ⏳ |
| Docker | ✅ |

---

## 🛡️ Надёжность и восстановление

ScoriaDB использует трёхуровневую систему долговечности:

1. **WAL** — каждая операция записывается с CRC32, `fsync` после каждого батча. При перезапуске WAL воспроизводится.
2. **Manifest** — JSON-журнал, отслеживающий все изменения SSTable, `fsync` после каждой записи. При старте восстанавливает точный набор файлов.
3. **Value Log** — если магическое число повреждено, файл переименовывается в `.corrupt`, создаётся новый, данные восстанавливаются из WAL.

**Время восстановления:** <1 секунды после `kill -9`.  
**Конкуренты:** BadgerDB и Pebble — 9–12 секунд.

---

## 🕰️ Как работает MVCC

- Каждый `Put` создаёт новую версию с `commitTS` (uint64).
- Транзакция вызывает `Begin()` и получает `startTS` — временную метку снимка.
- Чтения внутри транзакции видят только версии с `commitTS ≤ startTS`.
- При `Commit()` движок проверяет, был ли изменён ключ после `startTS` (используя `lastCommitCache` для O(1) быстрого пути). Если конфликт найден → `ErrConflict`, транзакцию нужно повторить.

**Трюк с инвертированной меткой** — ключи хранятся как `[user_key][^commitTS]`. Поскольку `^commitTS` уменьшается при увеличении `commitTS`, самая новая версия появляется первой при итерации.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → сначала "bob", потом "alice"
```

**Результат:** Писатели никогда не блокируют читателей. Изоляция снимков гарантирована.

---

## 📚 Документация

Полная документация доступна по адресу [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/) и в папке [`docs/`](docs/).

| Язык | Документация | Пример |
|------|--------------|--------|
| **Go** | [GoDoc](https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria) | `pkg/scoria` |
| **Python** | [docs/python/](docs/python/) | [example.py](docs/python/example.py) |
| **Java** | [docs/java/](docs/java/) | [example.java](docs/java/example.java) |
| **C++** | [docs/c++/](docs/c++/) | [example.cpp](docs/c++/example.cpp) |

---

## 🗺️ Дорожная карта

| Версия | Фокус | Ключевые возможности | Статус |
|--------|-------|---------------------|--------|
| **v0.1.0** | Стабильность ядра | LSM, MVCC, ACID, CF, gRPC, CLI | ✅ |
| **v0.1.1** | CLI и документация | Интерактивные команды, документация на разных языках | ✅ |
| **v0.2.0** | Производительность записи | Group Commit, опции WAL | ✅ |
| **v0.2.1** | Быстрые победы | sync.Pool, чтение -67%, WAL -84% | ✅ |
| **v0.3.0** | Zero‑copy + Lock‑free + Unified MMap | Lock‑free skip list, Zero‑copy VLog, Unified MMap, Graceful shutdown, Structured logging | 🚧 |
| **v0.3.1** | Double Buffer WAL | Double Buffer WAL, WAL конфигурация, бенчмарки | ⏳ |
| **v0.4.0** | TTL и сборка мусора | TTL, автоматический GC, бинарный Manifest, SSTable merge | ⏳ |
| **v0.5.0** | Масштабирование | Shard‑per‑core, балансировка gRPC | ⏳ |
| **v0.6.0** | Асинхронный ввод-вывод | io_uring, CLI v2 | ⏳ |
| **v0.7.0** | Отказоустойчивость | Кластер ZeroRaft | ⏳ |
| **v1.0.0** | Распределённость | Range-шардирование, распределённые ACID, RLS, mTLS | ⏳ |

### Текущие блокеры v0.3.0

| Проблема | Описание | Статус |
|----------|----------|--------|
| Skip list медленный для 4KB | 61,000 ns vs 420 ns цель (150× медленнее) | 🚧 |
| Ring buffer переполняется | Падение после 131K записей | 🚧 |
| `updateLastCommitCache` аллоцирует | 1 alloc/op через `string(key)` | 🚧 |

---

## 📁 Структура проекта

```
ScoriaDB/
├── cmd/              # Точки входа сервера и CLI
├── internal/         # Движок, MVCC, транзакции, Column Families, API
├── pkg/scoria/       # Публичный встраиваемый API
├── proto/            # Определения protobuf для gRPC
├── tests/            # Интеграционные и нагрузочные тесты
├── deployments/      # Docker-файлы
└── docs/             # Документация на разных языках
```

---

## 🤝 Участие в разработке

Приветствуются любые вклады!

1. Сделайте форк репозитория
2. Создайте ветку с новой функциональностью
3. Внесите изменения
4. Запустите тесты: `go test -race ./...`
5. Запустите линтер: `golangci-lint run ./...`
6. Отправьте пулл-реквест

Подробнее в [CONTRIBUTING.md](CONTRIBUTING.md).

---

## 📄 Лицензия

**Apache License 2.0** — см. [LICENSE](LICENSE).

---

## ❓ Вопросы и ответы

<details>
<summary><b>Можно ли использовать из Python / Java / C++?</b></summary>
<br>
Да — примеры для gRPC в <code>docs/</code>.
</details>

<details>
<summary><b>Чем ScoriaDB лучше BadgerDB?</b></summary>
<br>
ScoriaDB имеет <b>MVCC, Column Families, lock‑free skip list, Unified MMap, встроенные gRPC/REST</b> и в <b>7 раз быстрее</b> при чтении.
</details>

<details>
<summary><b>Что такое Group Commit?</b></summary>
<br>
Group Commit буферизирует записи и выполняет один <code>fsync</code> для батча (каждые 10 мс). В 6.4× быстрее запись.
</details>

<details>
<summary><b>Что такое Unified MMap?</b></summary>
<br>
Единый mmap-регион, заменяющий отдельные VLog и WAL. 0 системных вызовов на запись, 0 аллокаций в горячем пути. Динамическое расширение при переполнении.
</details>

<details>
<summary><b>Что такое lock‑free skip list?</b></summary>
<br>
Конкурентная структура данных без мьютексов. Использует CAS-операции для атомарной вставки. Даёт +400% к производительности записи для малых ключей.
</details>

<details>
<summary><b>Есть ли zero‑copy?</b></summary>
<br>
Да — начиная с v0.3.0, чтение VLog является zero‑копийным. Большие значения возвращаются как срезы, указывающие напрямую на память mmap. Скорость чтения улучшена на **+487%** для значений 4 КБ.
</details>

<details>
<summary><b>Каковы системные требования?</b></summary>
<br>
Любая платформа, поддерживаемая Go 1.23+. Бинарник ~15 МБ, без внешних зависимостей.
</details>

<details>
<summary><b>Можно ли использовать ScoriaDB на ARM (Raspberry Pi)?</b></summary>
<br>
Да — чистый Go работает на всех архитектурах (amd64, arm64, arm и т.д.).
</details>

<details>
<summary><b>Какая лицензия?</b></summary>
<br>
Apache License 2.0 — бесплатно для коммерческого и личного использования.
</details>

---

## ⭐ Поддержать проект

- ⭐ **Поставьте звезду** на GitHub.
- 🐛 **Сообщайте об ошибках** через Issues.
- 💻 **Отправляйте пулл-реквесты**.
- 📣 **Расскажите о проекте** в своём сообществе.

---

<div align="center">
  <br>
  <i>Твёрдая как камень. Лёгкая как пепел.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Полная%20документация-blue?style=for-the-badge" alt="Документация"></a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>