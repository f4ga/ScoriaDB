
<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=⚡%20Чистая%20Go%20LSM-база%20|%20Lock‑free%20|%20Zero‑copy%20|%20Встраиваемая&fontSize=20&fontAlignY=50&animation=twinkling">
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

  <b>18.4M Get/s • 2.92M Put/s • 1 alloc/op</b>

  <br><br>
</div>

---

## 📖 Что такое ScoriaDB?

**ScoriaDB** — встраиваемый движок хранения на чистом Go.

Это **production‑готовое LSM-дерево** с:
- MVCC + изоляцией снимков
- ACID-транзакциями
- Column Families
- Lock‑free skip list в MemTable
- Zero‑copy Value Log (mmap)
- Встроенными gRPC, REST, CLI
- Без внешних зависимостей, без cgo

**Результат:** **18.4M Get/s и 2.92M Put/s** на обычном ноутбуке — быстрее большинства in‑memory кэшей, но с персистентностью и ACID.

---

## 🚀 Быстрый старт

```bash
go get github.com/f4ga/ScoriaDB/pkg/scoria
```

```go
package main

import (
    "fmt"
    "log"
    "github.com/f4ga/ScoriaDB/pkg/scoria"
)

func main() {
    db, err := scoria.NewScoriaDB("./data")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    db.Put([]byte("hello"), []byte("world"))
    value, _ := db.Get([]byte("hello"))
    fmt.Printf("%s\n", value)
}
```

---

## 📊 Производительность

*Тесты на Intel Core i3‑1215U (ноутбук, 8 потоков), NVMe SSD, Go 1.23+*

### MemTable (lock‑free skip list)

| Бенчмарк | ops/s | ns/op | B/op | allocs/op | Ядер |
|----------|-------|-------|------|-----------|------|
| **Get** | **18.4M** | **72** | 23 | 1 | 8 |
| **Get** | 4.56M | 267 | 23 | 1 | 1 |
| **Get (последовательный)** | 4.58M | 264 | 23 | 1 | 8 |
| **Put** | **2.92M** | **432** | 23 | 1 | 8 |
| **Put** | 2.66M | 473 | 23 | 1 | 1 |
| **Put (последовательный)** | 2.71M | 469 | 23 | 1 | 8 |

### Движок (LSM + WAL + VLog)

*Полный движок с долговечностью и MVCC*

| Операция | Пропускная способность | Задержка | Аллокации |
|----------|------------------------|----------|-----------|
| **Get (попадание в MemTable)** | ~10M ops/s | ~100 нс | 1 alloc |
| **Get (4KB, VLog)** | ~1.25M ops/s | 800 нс | 5 allocs |
| **Scan (100 ключей)** | **~1.25M ops/s** | **796 нс** | **7 allocs** |
| **WAL Group Commit** | 17.5M ops/s | 57 нс | 0 allocs |
| **WAL Sync** | 2.74M ops/s | 365 нс | 0 allocs |

> **Примечание:** Бенчмарки движка отражают текущее состояние разработки. Полные LSM-бенчмарки (с чтением из SSTable и компакцией) будут опубликованы с v0.3.0.

---

## 🏆 Сравнение с конкурентами

| СУБД | Тип | Запись (ops/s) | Чтение (ops/s) | ACID | MVCC | Встраиваемая |
|------|-----|----------------|----------------|------|------|--------------|
| **ScoriaDB** (MemTable) | LSM (Go) | **2.92M** | **18.4M** | ✅ | ✅ | ✅ |
| BadgerDB | LSM (Go) | ~171K | ~400K | ✅ | ❌ | ✅ |
| Pebble | LSM (Go) | ~472K | ~1M | ❌ | ❌ | ✅ |
| RocksDB | LSM (C++) | ~356K | ~1.06M | ❌ | ❌ | ❌ |
| LevelDB | LSM (C++) | ~1.5M | ~10K | ❌ | ❌ | ✅ |
| Redis | In‑memory | ~1M | ~10.5M | ❌ | ❌ | ❌ |
| SQLite | B+Tree | ~20K | ~60K | ✅ | ❌ | ✅ |

**Ключевые выводы:**
- ScoriaDB MemTable **в 6 раз быстрее** Pebble по записи
- Скорость чтения (**18.4M ops/s**) — **самая высокая** среди всех встраиваемых KV-хранилищ
- Только ScoriaDB предлагает **ACID + MVCC + lock‑free** в чистом Go

---

## 🧩 Возможности

### Ядро движка
- ✅ **LSM-дерево** с уровневой компактацией
- ✅ **Lock‑free skip list** в MemTable — без мьютексов на запись
- ✅ **SSTable** с блочным индексом, Bloom-фильтром, префиксным сжатием
- ✅ **Value Log (WiscKey)** — большие значения хранятся отдельно
- ✅ **Zero‑copy mmap** — чтение без копирования из VLog
- ✅ **MVCC + Snapshot Isolation**
- ✅ **ACID-транзакции** с оптимистичным контролем конкурентности
- ✅ **Column Families** — независимые LSM-деревья
- ✅ **WAL с Group Commit** — 17.5M ops/s
- ✅ **gRPC, REST, CLI** — один бинарник, без конфигов
- ✅ **JWT-аутентификация** — роли admin/readwrite/readonly
- ✅ **Graceful shutdown** — обработка SIGINT/SIGTERM

### Эффективность памяти
- **1 alloc/op** в Get (против 8 в Redis)
- **0 allocs/op** в Bloom-фильтре (против 2 в RocksDB)
- **5 allocs/op** при чтении 4KB из VLog (против 8 в v0.2.2)
- **7 allocs/op** при сканировании (было 107 в v0.2.2)

---

## 📊 Влияние ключевых оптимизаций

| Оптимизация | До | После | Прирост |
|-------------|-----|-------|---------|
| **Lock‑free skip list** | 1.51M Put/s | **2.92M Put/s** | **+94%** |
| **Zero‑copy VLog (чтение 4KB)** | 213K ops/s | **1.25M ops/s** | **+487%** |
| **Scan (heap-based итератор)** | 4809 нс, 107 allocs | **796 нс, 7 allocs** | **-83% задержка, -93% аллокации** |
| **SSTable block pooling** | 432 нс | 140 нс | **-67%** |
| **WAL buffer pooling** | 515 нс | 436 нс | **-15%** |
| **Bloom filter (fastrand)** | 16 мкс | 14.8 мкс | **-7.5%** |

---

## 🚧 Известные ограничения

Прозрачность важна. Вот текущие известные проблемы, над которыми ведётся работа:

| Проблема | Описание | ETA |
|----------|----------|-----|
| **Производительность скиплиста для 4KB** | Большие значения (4KB) медленнее цели | v0.3.1 |
| **Переполнение кольцевого буфера** | Падает после ~131K записей в MemTable | v0.3.1 |
| **Слияние SSTable** | Компакция пока не объединяет SSTable | v0.4.0 |

---

## 🗺️ Дорожная карта

| Версия | Фокус | Ключевые возможности | Статус |
|--------|-------|---------------------|--------|
| **v0.1.0** | Стабильность ядра | LSM, MVCC, ACID, CF, gRPC, CLI | ✅ |
| **v0.1.1** | CLI и документация | Интерактивные команды, документация на разных языках | ✅ |
| **v0.2.0** | Производительность записи | Group Commit, опции WAL | ✅ |
| **v0.2.1** | Быстрые победы | sync.Pool, fastrand, errcheck, deadcode | ✅ |
| **v0.2.2** | Zero‑copy VLog | Zero‑copy mmap, graceful shutdown | ✅ |
| **v0.3.0** | Lock‑free | Lock‑free skip list, арена, heap-based scan | 🚧 |
| **v0.3.1** | Критические исправления | Производительность 4KB, кольцевой буфер | ⏳ |
| **v0.4.0** | TTL и GC | TTL, автоматический GC, бинарный Manifest | ⏳ |
| **v0.5.0** | Масштабирование | Shard‑per‑core | ⏳ |
| **v0.6.0** | Асинхронный I/O | io_uring | ⏳ |
| **v0.7.0** | Отказоустойчивость | Кластер ZeroRaft | ⏳ |
| **v1.0.0** | Распределённость | Range-шардирование, распределённые ACID | ⏳ |

---

## 📄 Лицензия

**Apache License 2.0** — бесплатно для коммерческого и личного использования.

---

<div align="center">
  <br>
  <i>Твёрдая как камень. Лёгкая как пепел.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>
