
<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn" alt="Header">
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&section=header&text=🔥%20Встраиваемая%20LSM-база%20данных%20на%20Go%20|%20Крепкая%20как%20камень%2C%20лёгкая%20как%20пепел&fontSize=20&fontAlignY=50&animation=twinkling" alt="Subtitle">
  <br><br>

  [![CI](https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg)](https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml)
  [![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://go.dev/)
  [![License](https://img.shields.io/badge/license-Apache%202.0-blue)](LICENSE)

  <div align="center">

  [🇬🇧 English](README.md) &nbsp;&nbsp;|&nbsp;&nbsp; [🇷🇺 Русский](README_RU.md)

  </div>

  <br>
  <table align="center" style="font-size: 1.4em; line-height: 2;">
    <tr><td>📖</td><td><a href="#-что-такое-scoriadb">Что такое ScoriaDB</a></td></tr>
    <tr><td>👥</td><td><a href="#-для-кого-она-создана">Для кого она создана</a></td></tr>
    <tr><td>✨</td><td><a href="#-почему-scoriadb">Почему ScoriaDB</a></td></tr>
    <tr><td>📊</td><td><a href="#-бенчмарки">Бенчмарки</a></td></tr>
    <tr><td>📊</td><td><a href="#-сравнение-с-redis">Сравнение с Redis</a></td></tr>
    <tr><td>🧩</td><td><a href="#-возможности-и-функции">Возможности и функции</a></td></tr>
    <tr><td>🛡️</td><td><a href="#-надёжность-и-восстановление-после-сбоев">Надёжность и восстановление после сбоев</a></td></tr>
    <tr><td>🕰️</td><td><a href="#-как-работает-mvcc">Как работает MVCC</a></td></tr>
    <tr><td>🌐</td><td><a href="#-поддержка-языков">Поддержка языков</a></td></tr>
    <tr><td>📈</td><td><a href="#-прогресс-mvp">Прогресс MVP</a></td></tr>
    <tr><td>📁</td><td><a href="#-структура-проекта">Структура проекта</a></td></tr>
    <tr><td>🗺️</td><td><a href="#-дорожная-карта">Дорожная карта</a></td></tr>
    <tr><td>❓</td><td><a href="#-faq">FAQ</a></td></tr>
    <tr><td>🤝</td><td><a href="#-поддержать-проект">Поддержать проект</a></td></tr>
  </table>
</div>

---

## 📖 Что такое ScoriaDB?

**ScoriaDB** — это гибридная key‑value база данных, которая стирает грань между легковесной встраиваемой библиотекой и полноценной сетевой СУБД.

- **Как встраиваемая библиотека** — компилируется прямо в ваш Go-процесс, давая максимальную скорость без внешних зависимостей.
- **Как готовый сервер** — предоставляет встроенные gRPC, CLI и Web UI, не требуя ни строчки дополнительного кода.

Она создана, чтобы вы не выбирали между «быстрой, но тупой» встраиваемой БД и «удобной, но громоздкой» сетевой СУБД.

---

## 👥 Для кого она создана?

ScoriaDB будет полезна:

- **Go-разработчикам**, которые хотят добавить персистентное KV-хранилище в свой микросервис, CLI-утилиту или агент — без лишней инфраструктуры.
- **IoT и Edge-инженерам** — когда нужно локальное хранилище на устройстве, но при этом важен удалённый доступ и мониторинг.
- **Разработчикам на Python, Java, C++** — доступ к данным из любого языка через gRPC без танцев с cgo и обёртками.
- **Командам с микросервисной архитектурой** — один сервер, много клиентов на разных языках, единый API.
- **Тем, кто строит прототипы** — встроенные интерфейсы (CLI, Web UI) позволяют сразу видеть данные.
- **Всем, кто устал поднимать Redis для простого кэша** или дописывать HTTP-обвязку к BoltDB.

---

## ✨ Почему ScoriaDB?

| Преимущество | Что это даёт |
| :--- | :--- |
| **Молниеносное чтение (embedded-режим)** | ~150 ns/op для `Get` операций в памяти. |
| **Гибридное хранение (WiscKey)** | Большие значения не засоряют LSM-дерево; читаются напрямую из mmap (zero‑copy). |
| **ACID-транзакции** | Snapshot Isolation, интерактивные транзакции, атомарный WriteBatch. |
| **Встроенный сервер** | gRPC API, Web UI и CLI доступны сразу — без написания сетевого кода. |
| **Кросс-языковой доступ** | 12+ языков через gRPC — Python, Java, C++, Rust, Node.js, C# и другие. |
| **Column Families** | Логически разделяйте данные с разными настройками компакшена. |
| **Надёжность (Manifest + WAL)** | Восстановление после сбоев без потери метаданных и данных. |
| **Чистый Go** | Без cgo, без внешних зависимостей — просто `go get`. |

---

## 📊 Бенчмарки

Измерено на Intel Core i3-1215U (8 потоков), Go 1.23+, Linux amd64.
Запуск: `go test -bench=. ./internal/engine ./pkg/scoria`

| Операция | Размер значения | Время (ns/op) | Пропускная способность |
|---|---|---|---|
| `Put` (small) | < 64 байт | **1 070 ns** | ~ 935 000 ops/s |
| `Put` (large) | 4 КБ (Value Log) | **4 785 ns** | ~ 209 000 ops/s |
| `Get` (hit) | ключ в MemTable | **~150 ns** | ~ 6 600 000 ops/s |
| `ScoriaPut` | через публичный API | **1 063 ns** | накладные расходы < 1% |
| `ScoriaGet` | через публичный API | **144 ns** | накладные расходы ~ 5% |

> Чтение на уровне BoltDB (~100–200 ns), но с MVCC, транзакциями и конкурентной записью.
> Запись — около 1 млн ops/s для одиночных `Put` без батчинга.
> Накладные расходы публичного API минимальны — интерфейс `DB` почти прозрачен.

---

## 📊 Сравнение с Redis

ScoriaDB **не является** заменой Redis. Redis — это удалённая, in-memory платформа данных; ScoriaDB — встраиваемая, дисково-ориентированная база данных. Однако в однопользовательской пропускной способности и задержке чтения ScoriaDB напрямую конкурирует с Redis Community Edition.

**Доверенные бенчмарки Redis CE** (Alibaba Cloud, 2025) :
- `GET`: ~204 690 ops/sec (средн. ~0,31 мс)
- `SET`: ~142 376 ops/sec (средн. ~0,45 мс)

**Бенчмарки ScoriaDB** (локально, встроенный режим, без сетевых накладных расходов):
- `Get`: ~6 600 000 ops/sec (~150 ns/op)
- `Put`: ~935 000 ops/sec (~1 070 ns/op)

**Почему такая разница?**
1. **Отсутствие сетевого стека.** ScoriaDB работает внутри вашего процесса. Redis добавляет накладные расходы TCP (~0,1–0,2 мс на запрос).
2. **Одна машина, один клиент.** Наш бенчмарк измеряет чистую производительность движка; бенчмарки Redis измеряют производительность сервера под нагрузкой.

**Когда ScoriaDB быстрее:**
- Локальные, встроенные сценарии, где сетевая задержка неприемлема.
- Микросервисы, которым нужны микросекундные чтения без внешнего кэша.

**Когда Redis выигрывает:**
- Распределённое кэширование на множестве серверов.
- Продвинутые структуры данных (streams, pub/sub, sorted sets и т.д.) .
- Горизонтальное масштабирование через Redis Cluster.

| Характеристика | ScoriaDB | Redis CE |
| :--- | :--- | :--- |
| **Развёртывание** | Встраиваемая библиотека или отдельный сервер | Отдельный серверный процесс |
| **Сетевые накладные расходы** | Отсутствуют (embedded-режим) | TCP (0,1–0,2 мс+) |
| **Задержка чтения** | ~150 ns | ~0,24–0,31 мс  |
| **Задержка записи** | ~1 070 ns | ~0,45 мс  |
| **Персистентность данных** | Полная (WAL, Manifest, fsync) | Опциональная (RDB, AOF) |
| **Транзакции** | ACID, Snapshot Isolation | Отсутствуют (pipelining) |
| **Поддержка языков** | gRPC (12+ языков) | Нативный протокол (клиентские библиотеки) |

---

## 🧩 Возможности и функции

### Storage Engine
| Функция | Описание |
| :--- | :--- |
| **LSM-дерево** | Отсортированная MemTable (B‑tree) с периодическим сбросом в SSTable на диск. |
| **WAL (Write‑Ahead Log)** | Каждая операция записывается в журнал с контрольной суммой CRC32 перед попаданием в MemTable. |
| **Value Log (WiscKey)** | Значения > 64 байт выносятся в отдельный append‑only файл; mmap для zero‑copy чтения. |
| **SSTable** | Блочный индекс, префиксное сжатие ключей, фильтр Блума, диапазонный фильтр (min/max ключ). |
| **Leveled Compaction** | Фоновое слияние SSTable для освобождения места и удаления tombstone. |
| **Сжатие** | Snappy и Zstd на уровне блоков SSTable. |

### Транзакции и версионность
| Функция | Описание |
| :--- | :--- |
| **MVCC** | Многоверсионное конкурентное управление с инвертированными временными метками. |
| **Snapshot Isolation** | Чтение видит консистентный снимок на `startTS`; писатели никогда не блокируют читателей. |
| **Интерактивные транзакции** | `Begin()` → `Get`/`Put`/`Delete` → `Commit()`/`Rollback()` с оптимистичной блокировкой. |
| **WriteBatch** | Атомарная группа операций под единым `commitTS`. |
| **Обнаружение конфликтов** | При коммите проверяет, был ли изменён ключ после `startTS`. |

### Данные и организация
| Функция | Описание |
| :--- | :--- |
| **Column Families** | Независимые LSM-деревья с настройками компакшена для каждого CF. Атомарные записи между CF. |
| **Embedded Go API** | Чистый интерфейс `DB` в `pkg/scoria` для встраивания в Go-приложения. |

---

## 🛡️ Надёжность и восстановление после сбоев

**Manifest** — это журнал метаданных, который записывает каждое изменение состава файлов с атомарным `fsync`. **WAL** записывает каждое изменение данных с CRC32. При старте движок читает Manifest для восстановления точной структуры файлов и WAL для восстановления незафиксированных записей.

Вместе они гарантируют **полное восстановление после внезапного отключения питания** — без повреждения метаданных и потери подтверждённых записей.

---

## 🕰️ Как работает MVCC

**MVCC (Multi‑Version Concurrency Control)** означает, что каждая запись создаёт новую версию ключа вместо перезаписи старой. Каждая версия несёт временную метку (`commitTS`).

1. При `Put` создаётся новая версия ключа с `commitTS = <текущее время>`.
2. Когда транзакция вызывает `Begin()`, она получает `startTS` — снимок на этот момент.
3. Все чтения внутри транзакции видят только версии с `commitTS ≤ startTS`.
4. При `Commit()` движок проверяет, не был ли ключ изменён после `startTS` (обнаружение конфликтов).

**Почему это важно:**
- **Писатели никогда не блокируют читателей.**
- **Snapshot Isolation.** Консистентное представление данных даже при конкурентной записи.
- **Time Travel возможен** (запланирован на Release 2).

---

## 🌐 Поддержка языков

ScoriaDB предоставляет **gRPC API** на основе Protocol Buffers. Любой язык с поддержкой gRPC может работать с вашей базой данных.

### Шаги для любого языка
1. Установите gRPC и protobuf для вашего языка.
2. Скачайте `.proto` файл из репозитория.
3. Сгенерируйте клиентский код через `protoc`.
4. Используйте сгенерированный клиент — вызывайте методы как обычные функции.

### 🐹 Go (родной)
```go
import "github.com/f4ga/scoriadb/pkg/scoria"

db, _ := scoria.Open(scoria.DefaultOptions("/tmp/mydb"))
defer db.Close()

db.Put([]byte("ключ"), []byte("значение"))
val, _ := db.Get([]byte("ключ"))
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

### 🌍 Поддерживаемые языки
| Язык | Статус |
| :--- | :--- |
| Go | ✅ Нативный API + gRPC |
| Python | ✅ gRPC |
| Java | ✅ gRPC |
| C++ | ✅ gRPC |
| Rust | ✅ через `tonic` |
| Node.js / TypeScript | ✅ gRPC |
| C# (.NET) | ✅ gRPC |
| Ruby | ✅ gRPC |
| PHP | ✅ gRPC |
| Kotlin | ✅ gRPC |
| Swift | ✅ gRPC |
| Dart | ✅ gRPC |

---

## 📈 Прогресс MVP

| Категория | Компонент | Статус |
| :--- | :--- | :--- |
| **Ядро** | LSM-дерево (MemTable, WAL, Value Log) | ✅ Готово |
| | SSTable (Bloom, диапазонный фильтр) | ✅ Готово |
| | Manifest (журнал метаданных) | ✅ Готово |
| | VFS-абстракция | ✅ Готово |
| | Leveled Compaction | ✅ Готово |
| **Версионность** | MVCC (Snapshot Isolation) | ✅ Готово |
| **Транзакции** | WriteBatch | ✅ Готово |
| | Интерактивные транзакции | ✅ Готово |
| **Организация данных** | Column Families | ✅ Готово |
| **API** | Embedded Go API (вкл. Scan) | ✅ Готово |
| | gRPC API | ✅ Готово |
| | REST API + WebSocket | ✅ Готово |
| **Интерфейсы** | CLI-клиент (`scoria`) | 🔜 Далее |
| | Web UI (React) | 🔜 Далее |
| **Безопасность** | Аутентификация (JWT, роли) | ✅ Готово |
| | Rate Limiting | 🔜 Далее |
| **Мониторинг** | Prometheus-метрики | 🔜 Далее |
| | Health checks (`/health`, `/ready`) | 🔜 Далее |
| **DevOps** | Docker и docker‑compose | 🔜 Далее |
| **Качество** | CI/CD (GitHub Actions, линтинг) | ✅ Готово |
| | Бенчмарки (движок + API) | ✅ Готово |
| | Структура тестов (unit, integration) | ✅ Готово |
---

## 📁 Структура проекта

```
scoriadb/
├── cmd/
│   └── server/              # точка входа gRPC-сервера
├── internal/
│   ├── engine/              # ядро LSM: MemTable, SSTable, VLog, WAL, Manifest, компакшен
│   │   ├── sstable/         # чтение/запись SSTable, фильтр Блума, кодирование
│   │   ├── vfs/             # абстракция файловой системы (тестируемый слой)
│   │   └── tests/           # интеграционные тесты движка
│   ├── mvcc/                # кодирование MVCC-ключей с инвертированными метками времени
│   ├── txn/                 # транзакции (WriteBatch, интерактивные)
│   ├── cf/                  # реестр Column Families + batch
│   └── api/
│       └── grpc/            # реализация gRPC-сервера
├── pkg/scoria/              # публичный Embedded Go API (интерфейс DB)
│   └── tests/               # интеграционные тесты API
├── proto/                   # описание protobuf-сервиса
├── scoriadb/                # сгенерированный код protobuf для Go
├── tests/                   # end-to-end и интеграционные тесты
├── web/                     # React фронтенд (запланирован)
└── deployments/             # Docker и docker-compose (запланированы)
```

---

## 🗺️ Дорожная карта

### Текущий релиз: v1.0 MVP
- [x] Core LSM-движок с WiscKey
- [x] MVCC + ACID-транзакции
- [x] Column Families
- [x] Embedded Go API
- [x] gRPC API
- [x] CI/CD с бенчмарками

### Release 2
- [ ] **GC Value Log** — сборщик мусора для мёртвых записей в Value Log с битовым массивом и хеш-таблицей.
- [ ] **Raft-репликация** — распределённый режим с шардированием.
- [ ] **Time Travel запросы** — чтение ключей на любой момент в прошлом.
- [ ] **Row-Level Security (RLS)** — политики доступа на уровне префиксов ключей внутри Column Family.
- [ ] **Chaos Monkey** — fault injection тесты.
- [ ] **YCSB-бенчмарки** — throughput, latency, write amplification против BadgerDB и PebbleDB.

---

## ❓ FAQ

### 1. ScoriaDB — это библиотека или сервер?
**И то, и другое.** Вы можете встроить ScoriaDB как Go-библиотеку (`import "github.com/f4ga/scoriadb/pkg/scoria"`) или запустить как сервер с gRPC и Web UI доступом. Оба режима работают из одного бинарника.

### 2. Чем ScoriaDB отличается от BoltDB?
BoltDB использует B+ дерево, допускает только одного писателя за раз и не имеет встроенного сетевого доступа. ScoriaDB основана на LSM-дереве, поддерживает конкурентную запись, MVCC со Snapshot Isolation и предоставляет gRPC/CLI/Web UI «из коробки».

### 3. Чем ScoriaDB отличается от BadgerDB?
Обе используют WiscKey (Value Log). BadgerDB — только встраиваемая библиотека: без сервера, без интерактивных транзакций (только batch). ScoriaDB добавляет интерактивные транзакции, Column Families, Manifest и кросс-языковой gRPC доступ.

### 4. Почему ScoriaDB быстрее Redis в однопользовательских бенчмарках?
Redis — это удалённый сервер. Каждый `GET` проходит через TCP, epoll и сетевой стек . ScoriaDB работает внутри вашего процесса — `Get` это прямой вызов функции к B-дереву. Компромисс: Redis масштабируется в кластеры и предлагает гораздо больше структур данных .

### 5. Можно ли использовать ScoriaDB в продакшене?
Проект на стадии MVP. Мы активно работаем над стабилизацией, тестированием и бенчмарками. Для критичных систем рекомендуем дождаться первого стабильного релиза или тщательно тестировать под своей нагрузкой.

---

## 🤝 Поддержать проект

ScoriaDB — свободное ПО под лицензией Apache 2.0. Вы можете помочь:

- **⭐️ Поставив звезду** на GitHub — это отличная мотивация!
- **🐛 Сообщая об ошибках** через [Issues](https://github.com/f4ga/scoriadb/issues).
- **💻 Отправляя pull requests** — любое улучшение приветствуется.
- **📣 Рассказывая о проекте** в соцсетях или чатах.

Спасибо, что вы с нами!

---

<div align="center">
  <i>Крепкая как камень. Лёгкая как пепел.</i><br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>