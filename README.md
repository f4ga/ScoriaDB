# ScoriaDB

<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=47M%20GET/s%20•%20375K–2.1M%20PUT/s%20•%200%20allocations&fontSize=24&fontAlignY=50&animation=twinkling">
  <br><br>

  <a href="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml"><img src="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
  <a href="https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria"><img src="https://img.shields.io/badge/📚-GoDoc-007D9C?logo=go" alt="GoDoc"></a>
  <a href="https://f4ga.github.io/ScoriaDB/"><img src="https://img.shields.io/badge/📖-Документация-8A2BE2?style=for-the-badge" alt="Documentation"></a>
</div>

---

## 📖 Оглавление

1. [Для каких задач](#-для-каких-задач)
2. [Производительность](#-производительность)
3. [Быстрый старт](#-быстрый-старт)
4. [Возможности](#-возможности)
5. [Режимы записи](#-режимы-записи)
6. [Статус и надёжность](#-статус-и-надёжность)
7. [Планы развития](#-планы-развития)
8. [Тесты](#-тесты)
9. [Лицензия](#-лицензия)

---

## 🎯 Для каких задач

**ScoriaDB решает конкретные бизнес-задачи, где нужна максимальная скорость при ограниченных ресурсах.**

### Кэширование и сессии
- **Задача:** хранить миллионы сессий пользователей с чтением под 50 млн запросов/с
- **Альтернативы:** Redis — быстрый, но без персистентности; BadgerDB — персистентный, но в 118 раз медленнее
- **ScoriaDB:** 47M GET/s + персистентность на диск

### Высокочастотная запись
- **Задача:** логи, метрики, события — 2 млн записей/с с сохранением
- **Альтернативы:** Kafka — сложно, требует кластер; RocksDB — в 344 раза медленнее
- **ScoriaDB:** 2.1M PUT/s, встраивается в приложение

### Критичные данные без потерь
- **Задача:** балансы, платежи, транзакции — каждая запись должна быть на диске до ACK
- **Альтернативы:** PostgreSQL — медленно; RocksDB с sync — 1-2K оп/с
- **ScoriaDB:** 375K PUT/s с fsync на каждую запись

### Встраивание в Go-сервисы
- **Задача:** база данных внутри микросервиса, без отдельного процесса и сети
- **Альтернативы:** SQLite — медленно; BoltDB — нет параллельной записи
- **ScoriaDB:** одна строчка кода, 13 МБ, без зависимостей

### Обработка больших значений
- **Задача:** эмбеддинги, изображения, документы — 1.24 ГБ/с пропускной способности
- **Альтернативы:** S3 — медленно; RocksDB — WA 10-30×
- **ScoriaDB:** 19K записей/с по 64 КБ, WA ~5-12×

➡️ **[Примеры использования](https://f4ga.github.io/ScoriaDB/#%D0%B4%D0%BB%D1%8F-%D0%BA%D0%B0%D0%BA%D0%B8%D1%85-%D0%B7%D0%B0%D0%B4%D0%B0%D1%87)**

---

## ⚡ Производительность

**Оборудование:** Intel i3-1215U (8 потоков), 16 ГБ DDR4, NVMe SSD, Linux 6.8, Go 1.23.

**Воспроизводимо:** `go test -bench=. -benchtime=5s -count=10 -benchmem ./internal/engine/`

| Операция | QPS | Задержка | Аллокаций в куче |
|----------|-----|----------|------------------|
| GET (попадание в MemTable) | **47 232 965/с** | 24.6 нс | **0** |
| GET (ключ не найден) | **35 805 098/с** | 30.6 нс | **0** |
| PUT 16 Б (Group Commit) | **2 093 383/с** | 580 нс | 1 |
| PUT 16 Б (Strict Sync) | **375 000/с** | 3.2 мкс | 1 |
| PUT 4 КБ (Group Commit) | **510 000/с** | 2.88 мкс | 1 |
| VLog Read (mmap) | **6 767 000/с** | 175 нс | 1 |

**Что значат эти цифры для бизнеса:**

- **47M GET/s** — один сервер заменяет 7 серверов DragonflyDB
- **375K PUT/s с fsync** — в 187 раз быстрее RocksDB в режиме строгой синхронизации
- **0 аллокаций** — нет GC-пауз, предсказуемая задержка для биржевых приложений

➡️ **[Подробные бенчмарки и сравнение](https://f4ga.github.io/ScoriaDB/#%D0%BF%D1%80%D0%BE%D0%B8%D0%B7%D0%B2%D0%BE%D0%B4%D0%B8%D1%82%D0%B5%D0%BB%D1%8C%D0%BD%D0%BE%D1%81%D1%82%D1%8C)**

---

## 🚀 Быстрый старт

### В Go-приложении

```bash
go get github.com/f4ga/ScoriaDB/pkg/scoria@v0.3.0
```

```go
package main

import "github.com/f4ga/ScoriaDB/pkg/scoria"

func main() {
    db, _ := scoria.NewScoriaDB("./data")
    defer db.Close()
    
    // Запись
    db.Put([]byte("user:123"), []byte("alice@example.com"))
    
    // Чтение — 0 аллокаций
    email, _ := db.Get([]byte("user:123"))
    
    // Транзакция
    tx := db.NewTransaction()
    tx.Put([]byte("balance:123"), []byte("1000"))
    tx.Commit()
}
```

### Как сервер

```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
go build -o scoria-server ./cmd/server
./scoria-server
```

```bash
# Получить токен
export TOKEN=$(./scoria-cli admin auth admin 2027)

# Работа с данными
./scoria-cli --token=$TOKEN set hello world
./scoria-cli --token=$TOKEN get hello
```

➡️ **[Клиенты для 13 языков](https://f4ga.github.io/ScoriaDB/#%D0%BA%D0%BB%D0%B8%D0%B5%D0%BD%D1%82%D1%81%D0%BA%D0%B8%D0%B5-%D0%B1%D0%B8%D0%B1%D0%BB%D0%B8%D0%BE%D1%82%D0%B5%D0%BA%D0%B8)**

---

## 🧩 Возможности

| Возможность | Что даёт бизнесу |
|-------------|------------------|
| **ACID-транзакции** (SI) | Консистентность данных без ручных блокировок |
| **MVCC** | Чтение не блокирует запись — нет простоев |
| **Колоночные семейства** | Изоляция данных с разными паттернами доступа в одной БД |
| **0 аллокаций на GET** | Нет GC-пауз — предсказуемая задержка |
| **3 режима записи** | Выбор между скоростью и durability под каждую задачу |
| **JWT + роли** | Безопасность: admin, readwrite, readonly |
| **gRPC + REST** | Интеграция с любой инфраструктурой |

➡️ **[Полный список](https://f4ga.github.io/ScoriaDB/#%D0%B2%D0%BE%D0%B7%D0%BC%D0%BE%D0%B6%D0%BD%D0%BE%D1%81%D1%82%D0%B8)**

---

## 🛡️ Режимы записи

| Режим | Скорость | Задержка | Гарантия |
|-------|----------|----------|----------|
| **Group Commit** | **2.09M/с** | 580 нс | ≤10 мс данных при сбое питания |
| **WAL Sync** | **1.89M/с** | 640 нс | ≤1 мс данных |
| **Strict Sync** | **375K/с** | 3.2 мкс | **0 потерь** — каждая запись на диске до ACK |

**Где какой использовать:**

- **Group Commit:** кэши, сессии, логи, метрики
- **WAL Sync:** пользовательские данные, контент, стандартные приложения
- **Strict Sync:** платежи, балансы, заказы, критические счётчики

```go
// Group Commit — максимальная скорость
db, _ := scoria.NewScoriaDB("./data",
    scoria.WithGroupCommit(10*time.Millisecond),
)

// Strict Sync — максимальная надёжность
db, _ := scoria.NewScoriaDB("./data",
    scoria.WithSync(true),
    scoria.WithGroupCommitDisabled(),
)
```

➡️ **[Как выбрать режим](https://f4ga.github.io/ScoriaDB/#%D1%80%D0%B5%D0%B6%D0%B8%D0%BC%D1%8B-%D0%B7%D0%B0%D0%BF%D0%B8%D1%81%D0%B8)**

---

## 🏛️ Статус и надёжность

**ScoriaDB — это работающий продукт, который можно использовать для экспериментов и прототипов.**

Мы честно говорим о том, что работает, а что — в разработке.

### ✅ Что работает и проверено

| Компонент | Статус | Подтверждение |
|-----------|--------|---------------|
| LSM-движок с разделением ключей/значений | ✅ | 480+ тестов |
| Lock-free skip list + арена (0 аллокаций) | ✅ | Бенчмарки, `-race` |
| ACID-транзакции (Snapshot Isolation) | ✅ | 20+ сценариев |
| MVCC | ✅ | Интеграционные тесты |
| Колоночные семейства | ✅ | Сквозные тесты |
| WAL recovery | ✅ | Crash-тесты |
| gRPC + REST API | ✅ | 85% покрытия |
| JWT-аутентификация с ролями | ✅ | 90% покрытия |
| Value Log GC (ручной) | ✅ | Команда `admin gc` |

### 🟡 В разработке

| Компонент | Прогресс | Ожидание |
|-----------|----------|----------|
| Автоматическая GC Value Log | 40% | Октябрь 2026 |
| Compaction без перезаписи значений | 30% | Октябрь 2026 |
| TLS/mTLS | 20% | Ноябрь 2026 |
| Raft-кластер | 🔬 Проектирование | Q1-Q2 2027 |

### ⚠️ Ограничения текущей версии

- Нет автоматической сборки мусора — запускается вручную
- Нет кластеризации — только одна нода
- Нет встроенных Prometheus-метрик
- Write Amplification 5-12× (будет снижена до 1.05× в v0.5.0)

➡️ **[Подробный статус](https://f4ga.github.io/ScoriaDB/#%D1%81%D1%82%D0%B0%D1%82%D1%83%D1%81-%D0%B8-%D0%BD%D0%B0%D0%B4%D1%91%D0%B6%D0%BD%D0%BE%D1%81%D1%82%D1%8C)**

---

## 🗺️ Планы развития

### Q3 2026 — v0.4.0
- PUT → 0 аллокаций
- TTL (Time-To-Live)
- Бинарный поиск в SSTable

### Q4 2026 — v0.5.0
- Автоматическая GC Value Log
- Compaction только ключей (WA → 1.05×)
- TLS/mTLS
- Автоматический compaction

### Q1 2027 — v0.6.0
- io_uring / Direct I/O
- 100K одновременных соединений
- ZSTD-сжатие
- Prometheus-метрики

### Q2 2027 — v0.7.0
- **Raft-кластер (3-5 нод)**
- Автоматический failover
- Репликация записей
- Чтение с фолловеров

### Q3-Q4 2027 — v0.8.0 → v1.0.0
- Распределённые транзакции (2PC)
- Placement Driver
- CDC (Kafka/NATS)
- Row-Level Security
- Jepsen-сертификация

➡️ **[Детальная дорожная карта](https://f4ga.github.io/ScoriaDB/#%D0%BF%D0%BB%D0%B0%D0%BD%D1%8B-%D1%80%D0%B0%D0%B7%D0%B2%D0%B8%D1%82%D0%B8%D1%8F)**

---

## 🧪 Тесты

**480+ тестов** — это не просто галочка. Мы проверяем каждый коммит.

### Покрытие кода

| Пакет | Покрытие |
|-------|----------|
| `internal/api/grpc` | 85.6% |
| `internal/api/rest` | 82.7% |
| `internal/auth` | 90.3% |
| `internal/cf` | 83.9% |
| `internal/engine` | 63.8% |
| `internal/keys` | 100% |
| `internal/mvcc` | 95.0% |
| `internal/txn` | 79.4% |

### Crash-тесты

Проверяем, что БД восстанавливается после:
- Обрыва WAL (последние байты повреждены)
- Усечения WAL (файл обрезан)
- Повреждения манифеста
- Краша во время flush
- Краша во время compaction

Все тесты проходят.

### Run tests

```bash
go test ./internal/... -cover -v
go test -bench=. -benchtime=5s -count=10 -benchmem ./internal/engine/
```

➡️ **[Подробные результаты](https://f4ga.github.io/ScoriaDB/#%D1%82%D0%B5%D1%81%D1%82%D1%8B)**

---

## ❓ Частые вопросы

**Можно ли использовать в продакшене?**
Да, для некритичных нагрузок и прототипов. Для критичных систем — рекомендуем дождаться v0.5.0 (декабрь 2026) с автоматической GC и TLS.

**На каких платформах работает?**
Go поддерживает все основные платформы. ScoriaDB тестируется на Linux (amd64, arm64), macOS и Windows. Работает везде, где есть Go 1.23+.

**Есть ли поддержка Windows?**
Да, тесты проходят на Windows. Файловые операции используют стандартный `os` пакет.

**Сколько ресурсов потребляет?**
- Бинарник: 13 МБ
- MemTable: 4 МБ по умолчанию (настраивается)
- VLog: растёт с данными
- CPU: зависит от нагрузки

**Есть ли клиенты не на Go?**
Да. gRPC-клиенты для Go, Python, Java, C++, Rust, TypeScript, C#, Kotlin, Dart, PHP, Ruby, Swift, Objective-C.

**Лицензия?**
Apache 2.0 — можно использовать в коммерческих проектах без ограничений.

➡️ **[Полный FAQ](https://f4ga.github.io/ScoriaDB/#faq)**

---

## 📄 Лицензия

Apache License 2.0 — [подробнее](LICENSE).

---
## 🤝 Вклад в проект

Мы открыты к контрибьюциям! 
- [CONTRIBUTING.md](CONTRIBUTING.md)
- [GitHub Issues](https://github.com/f4ga/ScoriaDB/issues) 
- [GitHub Pull Requests](https://github.com/f4ga/ScoriaDB/pulls)

<div align="center">
  <br>
  <br><br>
  <a href="https://f4ga.github.io/ScoriaDB/">
    <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=8&height=60&text=📖%20Читать%20документацию&fontSize=28&fontAlignY=50" alt="Documentation">
  </a>
</div>