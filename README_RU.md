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

- [📖 Что такое ScoriaDB?](#-что-такое-scoriadb)
- [✨ Зачем ScoriaDB?](#-зачем-scoriadb)
- [🚀 Быстрый старт](#-быстрый-старт)
  - [Docker](#docker)
  - [Сборка из исходников](#сборка-из-исходников)
  - [Запуск сервера](#запуск-сервера)
  - [Использование CLI](#использование-cli)
  - [Встраивание в Go](#встраивание-в-go)
- [📊 Бенчмарки](#-бенчмарки)
  - [Влияние Group Commit](#влияние-group-commit)
- [📊 Сравнение с конкурентами](#-сравнение-с-конкурентами)
- [🧩 Возможности](#-возможности)
  - [Ядро хранилища](#ядро-хранилища)
  - [Долговечность и журналы](#долговечность-и-журналы)
  - [Транзакции и MVCC](#транзакции-и-mvcc)
  - [Column Families](#column-families)
  - [API и инструменты](#api-и-инструменты)
- [🛡️ Надёжность и восстановление](#️-надёжность-и-восстановление)
- [🕰️ Как работает MVCC](#️-как-работает-mvcc)
- [📚 Документация](#-документация)
- [🗺️ Дорожная карта](#️-дорожная-карта)
- [📁 Структура проекта](#-структура-проекта)
- [🤝 Участие в разработке](#-участие-в-разработке)
- [📄 Лицензия](#-лицензия)
- [❓ Вопросы и ответы](#-вопросы-и-ответы)
- [⭐ Поддержать проект](#-поддержать-проект)

---

## 📖 Что такое ScoriaDB?

**ScoriaDB** — это **встраиваемая key‑value база данных**, написанная на чистом Go.

Она сочетает:

- **LSM‑дерево** (MemTable, SSTable, многоуровневая компактация)
- **MVCC** с изоляцией снимков (читатели никогда не блокируют писателей)
- **ACID-транзакции** (интерактивные + WriteBatch)
- **Column Families** (независимые LSM-деревья внутри одной БД)
- **WAL + Manifest** для восстановления после сбоев
- **Value Log** в стиле WiscKey (эффективное хранение больших значений)

ScoriaDB работает как **библиотека** (импортируйте и встраивайте) или как **сервер** (gRPC, REST, CLI, WebSocket). Без cgo и внешних зависимостей.

---

## ✨ Зачем ScoriaDB?

| Возможность | Что даёт |
|-------------|----------|
| **Встраиваемость** | Чистый Go, без cgo, `go get` и запускайте |
| **Готовый сервер** | gRPC, REST, CLI, WebSocket — один бинарник |
| **ACID-транзакции** | Изоляция снимков, оптимистичный контроль конкурентности |
| **Column Families** | Логическая изоляция с независимой компактацией |
| **MVCC** | Читатели никогда не блокируют писателей |
| **Клиенты на разных языках** | gRPC поддерживает 13+ языков |
| **Надёжность по умолчанию** | WAL + fsync, Manifest, CRC32, fail‑safe VLog |
| **Быстродействие** | Чтение ~140 нс, запись ~750 нс (для малых ключей) |

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
# Получить JWT-токен
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

## 📊 Бенчмарки

**Оборудование:** Intel Core i3-1215U (8 потоков), NVMe SSD, Go 1.23+, Linux amd64.

| Операция | Размер | Время | Пропускная способность |
|----------|--------|-------|------------------------|
| **Put (малый)** | 16 Б | ~750 нс | **1.33M ops/s** |
| **Put (синхронный)** | 16 Б | ~1 070 нс | **935K ops/s** |
| **Put (большой)** | 4 КБ | ~4 785 нс | **209K ops/s** |
| **Get (попадание, MemTable)** | — | ~140 нс | **7.1M ops/s** |
| **Get (промах)** | — | ~310 нс | **3.2M ops/s** |
| **Scan (10k ключей)** | — | ~2.2 мс | ~450 ops/s |
| **WAL (Group Commit)** | — | ~95 нс | **10.5M ops/s** |

### Влияние Group Commit

| Режим | Пропускная способность | Ускорение |
|-------|------------------------|-----------|
| Синхронный (fsync на запись) | 935K ops/s | 1× |
| **Group Commit** | **1.43M ops/s** | **1.53×** |

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
- Скорость чтения (**7.1M ops/s**) — **самая высокая** среди встраиваемых KV-хранилищ.
- Только ScoriaDB и FoundationDB предлагают **ACID + MVCC** в этом сравнении.

---

## 🧩 Возможности

### Ядро хранилища

| Компонент | Статус |
|-----------|--------|
| MemTable (B‑tree) | ✅ |
| SSTable (блочный индекс, Bloom, префиксное сжатие) | ✅ |
| Многоуровневая компактация | ✅ |
| Value Log (WiscKey, >64 байт) | ✅ |
| Сжатие Snappy / Zstd | ✅ |

### Долговечность и журналы

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

1. **WAL** — каждая операция записывается с CRC32, `fsync` после каждого батча. При перезапуске WAL воспроизводится.
2. **Manifest** — JSON-журнал, отслеживающий все изменения SSTable, `fsync` после каждой записи.
3. **Value Log** — при повреждении файл переименовывается в `.corrupt`, создаётся новый, данные восстанавливаются из WAL.

**Время восстановления:** <1 секунды после `kill -9`.  
**Конкуренты:** BadgerDB и Pebble — 9–12 секунд.

---

## 🕰️ Как работает MVCC

- Каждый `Put` создаёт новую версию с `commitTS` (uint64).
- Транзакция при `Begin()` получает `startTS` — временную метку снимка.
- Чтения внутри транзакции видят только версии с `commitTS ≤ startTS`.
- При `Commit()` движок проверяет, был ли изменён ключ после `startTS` (`lastCommitCache` для O(1) быстрого пути).
- Конфликт → `ErrConflict` (требуется повтор).

**Трюк с инвертированной меткой** — ключи хранятся как `[user_key][^commitTS]`. Поскольку `^commitTS` уменьшается при увеличении `commitTS`, самая новая версия появляется первой при итерации.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → сначала "bob", потом "alice"
```

**Результат:** Писатели никогда не блокируют читателей.

---

## 📚 Документация

Полная документация находится в [`docs/`](docs/) и доступна по адресу [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/).

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
| **v0.2.0** | Производительность записи | Group Commit, опции WAL, бенчмарки | ✅ |
| **v0.3.0** | UI и TTL | Веб-интерфейс, TTL, lock‑free skip list | 🚧 |
| **v0.4.0** | Производительность | Zero‑copy VLog, автоматическая сборка мусора, бинарный Manifest | ⏳ |
| **v1.0.0** | Распределённость | Raft, шардирование, распределённые транзакции | ⏳ |

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
Версия v0.2.0 стабильна и протестирована под нагрузкой. Для 1000+ конкурентных писателей рекомендуется подождать v0.3.0 (lock‑free skip list).
</details>

<details>
<summary><b>Можно ли использовать из Python / Java / C++?</b></summary>
<br>
Да — примеры для gRPC доступны в <code>docs/</code>.
</details>

<details>
<summary><b>Чем ScoriaDB лучше BadgerDB?</b></summary>
<br>
ScoriaDB имеет <b>MVCC, Column Families, встроенные gRPC/REST</b> и в <b>7 раз быстрее</b> при чтении. У BadgerDB более зрелая сборка мусора для Value Log.
</details>

<details>
<summary><b>Почему запись медленнее чтения?</b></summary>
<br>
<code>fsync</code> гарантирует долговечность. Используйте WriteBatch или Group Commit (включён по умолчанию).
</details>

<details>
<summary><b>Есть ли zero‑copy?</b></summary>
<br>
Пока нет — планируется в v0.4.0.
</details>

<details>
<summary><b>Что такое Group Commit и почему он включён по умолчанию?</b></summary>
<br>
Group Commit буферизирует записи и выполняет один <code>fsync</code> для батча (каждые 10 мс). Это увеличивает пропускную способность записи в 4–5 раз, сохраняя долговечность. Отключить можно через <code>WALOptions{GroupCommitEnabled: false}</code>.
</details>

<details>
<summary><b>Что такое Column Families?</b></summary>
<br>
Это независимые LSM-деревья внутри одной базы данных. Они обеспечивают логическую изоляцию и позволяют настраивать компактацию отдельно для каждого типа данных.
</details>

<details>
<summary><b>Поддерживаются ли транзакции между Column Families?</b></summary>
<br>
Да. WriteBatch атомарен между несколькими Column Families благодаря общему WAL.
</details>

<details>
<summary><b>Какой уровень изоляции используется?</b></summary>
<br>
Изоляция снимков (Snapshot Isolation). Читатели видят согласованный снимок на момент <code>startTS</code>. Писатели никогда не блокируют читателей.
</details>

<details>
<summary><b>Как быстро происходит восстановление после сбоя?</b></summary>
<br>
Менее 1 секунды после <code>kill -9</code>. У BadgerDB и Pebble — 9–12 секунд.
</details>

<details>
<summary><b>Каковы системные требования?</b></summary>
<br>
Любая платформа, поддерживаемая Go 1.23+. Бинарник ~15 МБ, без внешних зависимостей.
</details>

<details>
<summary><b>Можно ли использовать ScoriaDB на ARM (Raspberry Pi)?</b></summary>
<br>
Да — чистый Go работает на всех архитектурах, поддерживаемых Go (amd64, arm64, arm и т.д.).
</details>

<details>
<summary><b>Как я могу помочь проекту?</b></summary>
<br>
См. <a href="CONTRIBUTING.md">CONTRIBUTING.md</a>. Особенно приветствуется помощь с автоматической сборкой мусора, lock‑free структурами данных, тестированием на Windows/macOS и веб-интерфейсом.
</details>

<details>
<summary><b>Будет ли веб-интерфейс?</b></summary>
<br>
Планируется в v0.3.0. Будет написан на Go + Alpine.js (без React/Node.js).
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
- 💻 **Отправляйте пулл-реквесты** — любое улучшение важно.
- 📣 **Расскажите о проекте** в своём сообществе.

---

<div align="center">
  <i>Твёрдая как камень. Лёгкая как пепел.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Полная%20документация-blue?style=for-the-badge" alt="Документация"></a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>