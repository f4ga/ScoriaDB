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
- **Group Commit в WAL** — в 6.4× быстрее долговечная запись без потери надёжности
- **Встроенный gRPC-сервер** — клиенты на 13+ языках «из коробки»
- **Долговечность по умолчанию** — fsync, CRC32, Manifest, отказоустойчивый VLog

---

## ✨ Зачем ScoriaDB?

| Возможность | Что даёт |
|-------------|----------|
| **Встраиваемость** | Чистый Go, без cgo — `go get` и начинайте использовать |
| **Готовый к production сервер** | gRPC, REST, CLI, WebSocket — один бинарник, без конфигов |
| **ACID-транзакции** | Изоляция снимков с оптимистичным контролем конкурентности |
| **Column Families** | Логическая изоляция данных с отдельной компактацией для каждой CF |
| **MVCC** | Читатели никогда не блокируют писателей — консистентные снимки |
| **Клиенты на разных языках** | gRPC-клиенты для 13+ языков (примеры для Python, Java, C++) |
| **Долговечность по умолчанию** | WAL + fsync, Manifest, CRC32, отказоустойчивый VLog |
| **Быстродействие** | 7.1M чтений/с, 12.4M WAL операций/с, 1.33M записей/с |

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
| **Put (малый)** | 16 Б | **1.33M ops/s** | ~750 нс |
| **Get (попадание, MemTable)** | — | **7.1M ops/s** | **~140 нс** |
| **Get (промах)** | — | 3.2M ops/s | ~310 нс |
| **Scan (10k ключей)** | — | ~450 ops/s | ~2.2 мс |
| **WAL Sync** | ~50 Б | 1.94M ops/s | 515 нс |
| **WAL Group Commit** | ~50 Б | **12.4M ops/s** | **80.8 нс** |

### Память и аллокации

| Операция | Память (B/op) | Аллокации (allocs/op) |
|----------|---------------|------------------------|
| **Put (малый)** | 321 B/op | 7 allocs/op |
| **Get (попадание, MemTable)** | **153 B/op** | 4 allocs/op |
| **WAL Sync** | 48 B/op | 1 alloc/op |
| **WAL Group Commit** | 49 B/op | 1 alloc/op |
| **Scan (10k ключей)** | 3.6 MB/op | 10k allocs/op |

### Влияние оптимизаций

| Оптимизация | Было | Стало | Улучшение |
|-------------|------|-------|-----------|
| **SSTable block pooling** | 432 нс | **140 нс** | **-67%** |
| **Память SSTable** | 227 B | **153 B** | **-32%** |
| **WAL Group Commit** | 515 нс | **80.8 нс** | **-84%** |
| **Параллельный WAL Group Commit** | 837 нс | **136 нс** | **-84%** |

Все бенчмарки воспроизводимы: `go test -bench=. -benchmem ./internal/engine`.

---

## 📊 Сравнение с конкурентами

| СУБД | Тип | Запись (ops/s) | Чтение (ops/s) | ACID | MVCC | Встраиваемая |
|------|-----|----------------|----------------|------|------|--------------|
| **ScoriaDB** | LSM (Go) | **1.33M** | **7.1M** | ✅ | ✅ | ✅ |
| BadgerDB | LSM (Go) | ~171K | ~400K | ✅ | ❌ | ✅ |
| Pebble | LSM (Go) | ~472K | ~1M | ❌ | ❌ | ✅ |
| RocksDB | LSM (C++) | ~356K | ~1.06M | ❌ | ❌ | ❌ |
| LevelDB | LSM (C++) | ~2.25M | ~10K | ❌ | ❌ | ❌ |
| LMDB | B+Tree | ~502K | ~1.45M | ✅ | ❌ | ✅ |
| SQLite | B+Tree | ~20K | ~60K | ✅ | ❌ | ✅ |
| FoundationDB | Распределённая | 1.87M | — | ✅ | ✅ | ❌ |

**Ключевые выводы:**

- ScoriaDB в **3 раза быстрее** Pebble и в **8 раз быстрее** BadgerDB по записи.
- Скорость чтения (**7.1M ops/s**) — **самая высокая** среди всех встраиваемых KV-хранилищ.
- Только ScoriaDB и FoundationDB предлагают **ACID + MVCC** в этом сравнении.

---

## 🧩 Возможности

### Движок хранения

| Компонент | Статус |
|-----------|--------|
| MemTable (B‑tree) | ✅ |
| SSTable (блочный индекс, Bloom, префиксное сжатие) | ✅ |
| Многоуровневая компактация | ✅ |
| Value Log (WiscKey, >64 байт) | ✅ |
| Сжатие Snappy / Zstd | ✅ |

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
| Метрики Prometheus | ✅ |
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
| **v0.3.0** | Производительность ядра | Lock‑free skip list, Double Buffer WAL, Zero‑copy VLog | 🚧 |
| **v0.4.0** | TTL и сборка мусора | TTL, автоматический GC, бинарный Manifest | ⏳ |
| **v0.5.0** | Масштабирование | Shard‑per‑core, балансировка gRPC | ⏳ |
| **v0.6.0** | Асинхронный ввод-вывод | io_uring, CLI v2 | ⏳ |
| **v0.7.0** | Отказоустойчивость | Кластер ZeroRaft | ⏳ |
| **v1.0.0** | Распределённость | Range-шардирование, распределённые ACID, RLS, mTLS | ⏳ |

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
<summary><b>Готова ли ScoriaDB к production?</b></summary>
<br>
v0.2.0 стабильна и протестирована под нагрузкой. Для 1000+ конкурентных писателей рекомендуется подождать v0.3.0 (lock‑free skip list).
</details>

<details>
<summary><b>Можно ли использовать из Python / Java / C++?</b></summary>
<br>
Да — примеры для gRPC в <code>docs/</code>.
</details>

<details>
<summary><b>Чем ScoriaDB лучше BadgerDB?</b></summary>
<br>
ScoriaDB имеет <b>MVCC, Column Families, встроенные gRPC/REST</b> и в <b>7 раз быстрее</b> при чтении.
</details>

<details>
<summary><b>Что такое Group Commit?</b></summary>
<br>
Group Commit буферизирует записи и выполняет один <code>fsync</code> для батча (каждые 10 мс). В 6.4× быстрее запись.
</details>

<details>
<summary><b>Есть ли zero‑copy?</b></summary>
<br>
Пока нет — планируется в v0.4.0.
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