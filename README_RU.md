<div align="center">
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=200&section=header&text=🪨%20ScoriaDB&fontSize=70&fontAlignY=40&animation=fadeIn">
  <br>
  <img src="https://capsule-render.vercel.app/api?type=rect&color=gradient&customColorList=1&height=60&text=🔥%20Встраиваемая%20LSM-база%20данных%20для%20Go%20|%20Крепкая%20как%20камень%2C%20лёгкая%20как%20пепел&fontSize=20&fontAlignY=50&animation=twinkling">
  <br><br>

  <!-- Бейджи -->
  <a href="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml"><img src="https://github.com/f4ga/ScoriaDB/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go" alt="Go Version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-Apache%202.0-blue" alt="License"></a>
  <a href="https://goreportcard.com/report/github.com/f4ga/ScoriaDB"><img src="https://goreportcard.com/badge/github.com/f4ga/ScoriaDB" alt="Go Report Card"></a>

  <br><br>
  <div>
    <a href="README.md"><img src="https://img.shields.io/badge/🇬🇧-English-blue?style=for-the-badge&logo=googletranslate" alt="English"></a>
    &nbsp;&nbsp;
    <a href="README_RU.md"><img src="https://img.shields.io/badge/🇷🇺-Русский-red?style=for-the-badge&logo=googletranslate" alt="Русский"></a>
  </div>

  <br>
  <a href="https://f4ga.github.io/ScoriaDB/"><img src="https://img.shields.io/badge/📖-Полная%20документация-blue?style=for-the-badge" alt="Documentation"></a>

  <br><br>
  <table align="center" style="font-size: 1.2em; line-height: 1.8;">
    <tr><td align="center">📖</td><td><a href="#-что-такое-scoriadb">Что такое ScoriaDB</a></td>
      <td align="center">👥</td><td><a href="#-для-кого">Для кого</a></td>
      <td align="center">✨</td><td><a href="#-почему-scoriadb">Почему ScoriaDB</a></td>
    </tr>
    <tr><td align="center">📊</td><td><a href="#-бенчмарки">Бенчмарки</a></td>
      <td align="center">📊</td><td><a href="#-сравнение-с-redis">Сравнение с Redis</a></td>
      <td align="center">🧩</td><td><a href="#-возможности-и-функции">Возможности и функции</a></td>
    </tr>
    <tr><td align="center">🛡️</td><td><a href="#-надёжность-и-восстановление">Надёжность и восстановление</a></td>
      <td align="center">🕰️</td><td><a href="#-как-работает-mvcc">Как работает MVCC</a></td>
      <td align="center">📚</td><td><a href="#-документация">Документация</a></td>
    </tr>
    <tr><td align="center">📈</td><td><a href="#-статус-релизов">Статус релизов</a></td>
      <td align="center">📁</td><td><a href="#-структура-проекта">Структура проекта</a></td>
      <td align="center">🗺️</td><td><a href="#-дорожная-карта-версий">Дорожная карта версий</a></td>
    </tr>
    <tr><td align="center">📄</td><td><a href="#-лицензия">Лицензия</a></td>
      <td align="center">❓</td><td><a href="#-faq">FAQ</a></td>
      <td align="center">🤝</td><td><a href="#-поддержать-проект">Поддержать проект</a></td>
    </tr>
  </table>
</div>

<br>

## 📖 Что такое ScoriaDB?

**ScoriaDB** — это встраиваемая key‑value база данных на чистом Go.  
Она объединяет **производительность LSM‑дерева**, **MVCC с ACID‑транзакциями**, **Column Families**, **WAL + Manifest для crash recovery** и **Value Log в стиле WiscKey** — всё в одном бинарнике без внешних зависимостей.

- **Как библиотека** – `import "github.com/f4ga/ScoriaDB/pkg/scoria"`. Вы получаете готовый LSM‑движок внутри своего Go‑процесса. Никакого cgo, никаких внешних сервисов.
- **Как сервер** – запустите `scoria-server`, и он сразу заговорит на gRPC (и предоставит доступ из **13 языков**), REST, WebSocket, CLI со множеством функций для администрирования.

**В чём уникальность ScoriaDB**  
- **Чистый Go без cgo** – легко собирать, кросс‑платформенно, полностью отлаживаемо.  
- **Первая Go‑нативная LSM с MVCC + Snapshot Isolation** – писатели никогда не блокируют читателей.  
- **Column Families** – независимые LSM‑деревья внутри одной БД, атомарные записи между CF.  
- **Group Commit в WAL** – в 4–5 раз более быстрая долговечная запись без потери надёжности.  
- **Готовый сервер** – gRPC, REST, CLI, WebSocket (и Web UI в планах).  
- **Мультиязычные клиенты** – готовые gRPC‑примеры для Python, Java, C++.

> Текущая стабильная версия – **v0.2.0** (Group Commit выпущен). Все ключевые компоненты протестированы и задокументированы.

## 👥 Для кого

| Тип пользователя | Зачем |
|:---|:---|
| **Go‑разработчик** | Встроить быстрое KV‑хранилище в свой сервис, CLI или агент – не нужен отдельный процесс БД. |
| **IoT / Edge инженер** | Локальное хранилище с удалённым доступом через gRPC/REST на устройствах с ограниченными ресурсами. |
| **Команда микросервисов** | Один сервер, клиенты на многих языках (gRPC). |
| **Аналитик логов** | Демо‑инструмент **Scorix** показывает, как эффективно индексировать и искать логи. |
| **Студент / пет-проектчик** | Изучить LSM, MVCC, компакшн на чистом и читаемом исходном коде. |

---

## ✨ Почему ScoriaDB?

| Преимущество | Что даёт на практике |
|:---|:---|
| **Встраиваемость** | Чистый Go, без cgo, не нужен `apt-get install rocksdb`. |
| **Готовый сервер** | gRPC, REST, CLI, WebSocket – просто запустите `scoria-server`. |
| **ACID‑транзакции** | Snapshot Isolation, интерактивные транзакции, атомарный WriteBatch. |
| **Column Families** | Изолированные LSM‑деревья – отдельный компакшн под каждый тип данных. |
| **MVCC** | Читатели никогда не блокируют писателей. |
| **Кросс‑языковой доступ** | gRPC‑клиенты для 12+ языков. |
| **Надёжность** | WAL + Manifest с fsync, CRC32, fail‑safe VLog. |
| **Производительность** | **Чтение ~150 нс, запись ~1 мкс** (маленькие ключи). |

---

## 📊 Бенчмарки

**Тестовая среда:** Intel Core i3-1215U (8 потоков), NVMe SSD, Go 1.23+, Linux amd64.  
**Команда:** `go test -bench=. -count=5 ./internal/engine ./pkg/scoria | benchstat`

| Операция | Размер значения | Время (нс/оп) | Пропускная способность (оп/с) |
|----------|----------------|---------------|-------------------------------|
| `engine.Put` (маленькое) | 16 Б | **1 070** | ~935 000 |
| `engine.Put` (большое, VLog) | 4 КБ | **4 785** | ~209 000 |
| `engine.Get` (попадание, MemTable) | – | **152** | ~6 580 000 |
| `engine.Get` (промах) | – | **310** | ~3 225 000 |
| **Group Commit WAL (последовательно)** | ~50 Б | **94.9 нс** | ~10 540 000 |

> **Пакетная запись** (WriteBatch из 100 операций) даёт **~970 000 оп/с** с полной durability – fsync амортизируется.  
> **Чтение никогда не тормозит** – даже при интенсивной конкурентной записи (MVCC).

### Влияние Group Commit на WAL

| Режим | Задержка (нс/оп) | Пропускная способность (оп/с) |
|:---|:---:|:---:|
| Синхронный (fsync на каждую запись) | 454 | 2 200 000 |
| **Group Commit (10 мс)** | **94.9** | **10 500 000** |

*Group Commit уже выпущен и включён по умолчанию в серверном режиме.*

---

## 📊 Сравнение с Redis

ScoriaDB **не** является заменой Redis – разные ниши. Redis: in‑memory кеш. ScoriaDB: дисковое, долговечное, встраиваемое KV.

| Характеристика | ScoriaDB (встраиваемая) | Redis CE (сетевая) |
|:---|:---|:---|
| Развёртывание | Библиотека или сервер | Отдельный сервер |
| Сетевые накладные расходы | нет | ~0.1–0.2 мс TCP |
| Задержка чтения | **~150 нс** | ~0.24–0.31 мс |
| Задержка записи (синхронная) | **~1 070 нс** | ~0.45 мс (AOF everysec) |
| Персистентность | **полный fsync** | опциональная (RDB/AOF) |
| Транзакции | **ACID + Snapshot Isolation** | нет (только pipelining) |
| MVCC | **есть** | нет |
| Column Families | **есть** | нет |

---

## 🧩 Возможности и функции

### Движок хранения
| Компонент | Статус |
|:---|:---:|
| MemTable (B‑tree) | ✅ |
| SSTable (блочный индекс, Bloom, префиксное сжатие) | ✅ |
| Leveled Compaction | ✅ |
| Value Log (WiscKey, >64 байт) | ✅ |
| Сжатие Snappy / Zstd | ✅ |

### Надёжность и журналы
| Компонент | Статус |
|:---|:---:|
| WAL + fsync + восстановление | ✅ |
| **Group Commit** (буферизованный fsync) | ✅ |
| Manifest + fsync | ✅ |
| CRC32 блоков | ✅ |
| Fail‑safe VLog | ✅ |

### Транзакции и MVCC
| Возможность | Статус |
|:---|:---:|
| MVCC, Snapshot Isolation | ✅ |
| Интерактивные транзакции | ✅ |
| WriteBatch | ✅ |
| Обнаружение конфликтов (lastCommitCache) | ✅ |

### Column Families
| Возможность | Статус |
|:---|:---:|
| Независимые LSM‑деревья | ✅ |
| Общий WAL для атомарных кросс‑CF записей | ✅ |

### API и инструменты
| Интерфейс | Статус |
|:---|:---:|
| Встраиваемый Go API (`DB`, `CFDB`) | ✅ |
| gRPC (стриминг Scan, транзакции) | ✅ |
| REST + WebSocket | ✅ |
| CLI (`scoria`) с интерактивной оболочкой | ✅ |
| JWT аутентификация (роли: admin/readwrite/readonly) | ✅ |
| Метрики Prometheus, эндпоинты health/ready | ✅ |
| Docker и docker‑compose | ✅ |

---

## 🛡️ Надёжность и восстановление

1. **WAL** – каждая операция добавляется с CRC32, `fsync` вызывается после каждого батча (или после группового сброса). При перезапуске WAL воспроизводится.
2. **Manifest** – JSON‑журнал, отслеживающий все изменения SSTable, `fsync` после каждой записи. При старте восстанавливает точный набор файлов.
3. **Value Log** – если магическое число повреждено, файл переименовывается в `.corrupt`, создаётся новый, данные восстанавливаются из WAL.

**Цена** – `fsync` дорог, но **Group Commit** снижает его влияние в 4–5 раз. Для высоких нагрузок на запись используйте WriteBatch.

---

## 🕰️ Как работает MVCC

- Каждый `Put` создаёт новую версию с `commitTS` (uint64).
- Транзакция при `Begin()` получает `startTS` – временную метку снапшота.
- Чтения внутри транзакции видят только версии с `commitTS ≤ startTS`.
- При `Commit()` движок проверяет, не был ли изменён каждый записанный ключ после `startTS` (используя `lastCommitCache` для O(1) быстрого пути). Если конфликт найден → `ErrConflict`, транзакцию нужно повторить.

**Трюк с инвертированной меткой** – ключи хранятся как `[user_key][^commitTS]`. Поскольку `^commitTS` уменьшается при увеличении `commitTS`, самая новая версия появляется первой в порядке итерации.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Scan → сначала "bob", затем "alice"
```

**Результат:** писатели никогда не блокируют читателей. Гарантируется Snapshot Isolation.

---

## 📚 Документация

Полная документация находится в папке [`docs/`](docs/) и также доступна по адресу [f4ga.github.io/ScoriaDB](https://f4ga.github.io/ScoriaDB/).

| Язык | Документация | Пример |
|:---|:---|:---|
| **Go (встраиваемый)** | [GoDoc](https://pkg.go.dev/github.com/f4ga/ScoriaDB/pkg/scoria) | `pkg/scoria` |
| **Python** | [python-doc.md](docs/python/python-doc.md) | [example.py](docs/python/example.py) |
| **Java** | [java-doc.md](docs/java/java-doc.md) | [example.java](docs/java/example.java) |
| **C++** | [cpp-doc.md](docs/c++/cpp-doc.md) | [example.cpp](docs/c++/example.cpp) |

**Быстрый старт через Docker**
```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
docker compose -f deployments/docker-compose.yml up --build
docker exec -it scoria-server ./scoria-cli admin auth admin admin
docker exec -it scoria-server ./scoria-cli --token <токен> set hello world
```

**Локальная сборка**
```bash
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli
./scoria-server &
TOKEN=$(./scoria-cli admin auth admin admin)
./scoria-cli --token "$TOKEN" set hello world
```

**Встраиваемый Go API**
```go
import "github.com/f4ga/ScoriaDB/pkg/scoria"

db, _ := scoria.NewScoriaDB("./data")
defer db.Close()
db.Put([]byte("hello"), []byte("world"))
```

---

## 📈 Статус релизов

### v0.2.0 – текущая стабильная (май 2026)

Этот релиз фокусируется на **производительности записи**, управлении durability и документации.

| Возможность / Улучшение | Описание |
|:---|:---|
| **Group Commit в WAL** | Буферизованная запись с периодическим fsync (интервал 10 мс). Увеличивает пропускную способность записи в 4–5 раз без потери durability. |
| **groupCommitWriter** | Асинхронный цикл сброса + тикер, настраиваемый интервал. |
| **Публичное API для опций WAL** | `OpenWALWithOptions` и `EngineOptions` позволяют включать Group Commit. |
| **Мультиязычная документация** | Полные gRPC примеры и руководства для **Python, Java, C++** (см. `docs/`). |
| **Расширенный бенчмарк‑сьют** | Сравнение синхронного и группового коммита, разные размеры значений. |
| **Тесты восстановления после краша** | Проверена durability при включённом Group Commit. |

> Все основные возможности v0.1.0 остаются (LSM, MVCC, транзакции, Column Families, gRPC/REST/CLI и т.д.). v0.2.0 обратно совместима.

---

## 📁 Структура проекта

```
scoriadb/
├── cmd/                     # точки входа сервера и CLI
├── internal/                # движок, mvcc, txn, cf, api
├── pkg/scoria/              # публичное встраиваемое API
├── proto/                   # gRPC protobuf определения
├── tests/                   # интеграционные и стресс-тесты
├── deployments/             # Docker файлы
└── docs/                    # мультиязычная документация
```

---

## 🗺️ Дорожная карта версий

| Версия | Фокус | Ключевые возможности | Плановый релиз |
|:---|:---|:---|:---|
| **v0.1.0** | Первый стабильный | LSM, MVCC, ACID, Column Families, gRPC, CLI, базовый GC | Апрель 2026 ✅ |
| **v0.1.1** | CLI и документация | Интерактивные команды (`create-cf`, `list-cf`, `whoami`, `stats`, история, экспорт), Python/Java/C++ документация | Май 2026 ✅ |
| **v0.2.0** | Производительность записи | **Group Commit** (WAL), опции WAL, тесты crash recovery | Май 2026 ✅ |
| **v0.2.1** | Мелкие фиксы и QoL | CI для Windows/macOS, `admin delete-user`, `admin get-user` | Июнь 2026 |
| **v0.3.0** | Web UI и TTL | React дашборд, TTL (время жизни) для записей, Group Commit по умолчанию | Q3 2026 |
| **v0.4.0** | Переработка ядра | Lock‑free skip list (вместо B‑tree), настоящий zero‑copy Value Log, автоматический инкрементальный GC | Q4 2026 |
| **v1.0.0** | Распределённый режим | Raft‑репликация, range‑шардирование, распределённые ACID‑транзакции (2PC), нативные структуры данных (Sorted Sets, Lists, JSON индексы) | 2027 |

> **Примечание:** Версии с отметкой ✅ уже выпущены. Дорожная карта может меняться в зависимости от обратной связи и активности контрибьюторов.

---

## 📄 Лицензия

**Apache License 2.0** – см. файл [LICENSE](LICENSE).  
Вы можете использовать, изменять, распространять и сублицензировать. Название ScoriaDB не может быть использовано для продвижения производных продуктов без разрешения.

---

## ❓ FAQ

**ScoriaDB готова к продакшену?**  
v0.2.0 стабильна и протестирована под нагрузкой. Для 1000+ конкурентных писателей подождите lock‑free skip list (v0.4.0).

**Почему запись медленнее чтения?**  
`fsync` гарантирует durability. Используйте WriteBatch или включите Group Commit (уже по умолчанию).

**Можно ли использовать из Python / Java / C++?**  
Да – см. [docs/](docs/) для полных gRPC примеров.

**Как ScoriaDB сравнивается с BadgerDB?**  
ScoriaDB предлагает **MVCC, Column Families, интерактивные транзакции**, встроенный gRPC/REST и значительно **более быстрое чтение** (6.6 млн оп/с против ~400 тыс.). BadgerDB имеет более зрелый GC Value Log, но ScoriaDB с Group Commit даёт лучшую пропускную способность записи в синхронном режиме.

**Zero‑copy работает?**  
Пока нет – текущая реализация копирует из mmap для избежания SIGSEGV. Настоящий zero‑copy запланирован на v0.4.0.

**Как я могу внести вклад?**  
См. [CONTRIBUTING.md](CONTRIBUTING.md). Особенно приветствуется помощь с автоматическим GC, lock‑free структурами данных, тестированием на Windows/macOS и Web UI.

---

## 🤝 Поддержать проект

- ⭐ **Поставьте звезду** на GitHub.
- 🐛 **Сообщайте об ошибках** через Issues.
- 💻 **Отправляйте pull request'ы** – каждое улучшение важно.
- 📣 **Расскажите о проекте** в своём сообществе.

---

<div align="center">
  <i>Крепкая как камень. Лёгкая как пепел.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Полная%20документация-blue?style=for-the-badge" alt="Documentation"></a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>