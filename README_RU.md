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

  <!-- Переключатель языков -->
  <br><br>
  <div>
    <a href="README.md"><img src="https://img.shields.io/badge/🇬🇧-English-blue?style=for-the-badge&logo=googletranslate" alt="English"></a>
    &nbsp;&nbsp;
    <a href="README_RU.md"><img src="https://img.shields.io/badge/🇷🇺-Русский-red?style=for-the-badge&logo=googletranslate" alt="Русский"></a>
  </div>

  <!-- Ссылка на документацию -->
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Полная%20документация-blue?style=for-the-badge" alt="Документация"></a>

  <!-- Оглавление -->
  <br>
  <table align="center" style="font-size: 1.2em; line-height: 1.8;">
    <tr>
      <td align="center">📖</td>
      <td><a href="#-что-такое-scoriadb">Что такое ScoriaDB</a></td>
      <td align="center">👥</td>
      <td><a href="#-для-кого">Для кого</a></td>
      <td align="center">✨</td>
      <td><a href="#-почему-scoriadb">Почему ScoriaDB</a></td>
    </tr>
    <tr>
      <td align="center">📊</td>
      <td><a href="#-бенчмарки">Бенчмарки</a></td>
      <td align="center">📊</td>
      <td><a href="#-сравнение-с-redis">Сравнение с Redis</a></td>
      <td align="center">🧩</td>
      <td><a href="#-возможности-и-функции">Возможности и функции</a></td>
    </tr>
    <tr>
      <td align="center">🛡️</td>
      <td><a href="#-надёжность-и-восстановление">Надёжность и восстановление</a></td>
      <td align="center">🕰️</td>
      <td><a href="#-как-работает-mvcc">Как работает MVCC</a></td>
      <td align="center">📚</td>
      <td><a href="#-документация">Документация</a></td>
    </tr>
    <tr>
      <td align="center">📈</td>
      <td><a href="#-критерии-выпуска-v010">Критерии выпуска v0.1.0</a></td>
      <td align="center">📁</td>
      <td><a href="#-структура-проекта">Структура проекта</a></td>
      <td align="center">🗺️</td>
      <td><a href="#-дорожная-карта-версий">Дорожная карта версий</a></td>
    </tr>
    <tr>
      <td align="center">📄</td>
      <td><a href="#-лицензия">Лицензия</a></td>
      <td align="center">❓</td>
      <td><a href="#-faq">FAQ</a></td>
      <td align="center">🤝</td>
      <td><a href="#-поддержать-проект">Поддержать проект</a></td>
    </tr>
  </table>
</div>

<br>

## 📖 Что такое ScoriaDB?

**ScoriaDB** — это встраиваемая key‑value база данных на чистом Go.  
Внутри — LSM‑дерево, MVCC, колоночные семейства, WAL и Value Log в стиле WiscKey.

- **Как библиотека** — импортируйте `github.com/f4ga/scoriadb/pkg/scoria` и получите LSM‑движок внутри своего процесса. Никаких внешних зависимостей, никакого cgo.
- **Как сервер** — запустите один бинарник, и он поднимет gRPC, REST, WebSocket и CLI. Готовый бэкенд для микросервисов на любом языке.

> Проект на финишной прямой к **v0.1.0**. Почти всё работает, тесты зелёные.

---

## 👥 Для кого

| Тип пользователя | Сценарий |
|:---|:---|
| **Go‑разработчик** | Встроить KV‑хранилище в свой сервис, CLI‑утилиту, агент или демон. Не хочется поднимать отдельную базу. |
| **IoT / Edge инженер** | Устройство с ограниченными ресурсами — нужно локальное хранилище с удалённым доступом через gRPC или REST. |
| **Команда микросервисов** | Один сервер ScoriaDB, клиенты на Python, Java, C++, Rust, Node.js, C# — через gRPC. |
| **Аналитик логов** | Встраиваемая база для индексации и поиска по логам (демо-инструмент Scorix уже работает). |
| **Студент / пет-проектчик** | Понять, как устроены LSM, MVCC, компакшн — исходный код открыт и читаем. |

---

## ✨ Почему ScoriaDB?

| Преимущество | Что даёт на практике |
| :--- | :--- |
| **Встраиваемость** | Чистый Go. Не нужен `apt-get install rocksdb`, нет cgo. |
| **Готовый сервер** | gRPC, REST, CLI, WebSocket — из коробки. Просто запустите `scoria-server`. |
| **ACID‑транзакции** | Snapshot Isolation, интерактивные транзакции, атомарный WriteBatch. |
| **Column Families** | Независимые LSM‑деревья — настраивайте компакшн под каждый тип данных. |
| **MVCC** | Читатели не блокируют писателей (конкурентность ещё не идеальна, но работает). |
| **Кросс‑языковой доступ** | gRPC-клиенты для 12+ языков (Python, Java, C++, …). |
| **Надёжность** | WAL + Manifest с fsync, CRC32 на каждый блок. Данные не теряются после `kill -9`. |
| **Производительность** | ~150 нс на чтение, ~1 мкс на запись (маленькие ключи). |

---

## 📊 Бенчмарки

*Тестовая машина: Intel Core i3-1215U (8 потоков), 16GB RAM, NVMe SSD, Go 1.23+, Linux amd64.*  
*Запуск: `go test -bench=. -count=5 ./internal/engine ./pkg/scoria | benchstat`*

| Операция | Размер значения | Время (нс/оп) | Пропускная способность |
|----------|----------------|---------------|------------------------|
| `engine.Put` (маленькое) | 16 байт | **1 070** | ~935 000 оп/с |
| `engine.Put` (большое) | 4 КБ (Value Log) | **4 785** | ~209 000 оп/с |
| `engine.Get` (попадание) | ключ в MemTable | **152** | ~6 580 000 оп/с |
| `engine.Get` (промах) | ключа нет | **310** | ~3 225 000 оп/с |
| `ScoriaDB.Put` (публичное API) | 16 байт | **1 063** | ~940 000 оп/с |
| `ScoriaDB.Get` (публичное API) | 16 байт | **144** | ~6 940 000 оп/с |

> **Пояснения:** пакетная запись амортизирует накладные расходы fsync — десятки микросекунд на запись на NVMe. Чтение никогда не тормозит даже при активной записи — работает MVCC.

---

## 📊 Сравнение с Redis

**Важно:** ScoriaDB **не** является заменой Redis. Redis — это in‑memory кеш; ScoriaDB — встраиваемое дисковое хранилище с транзакциями.

| Характеристика | ScoriaDB (встраиваемая) | Redis CE (сетевая) |
|:---|:---|:---|
| **Развёртывание** | Библиотека или сервер | Только сервер |
| **Сетевые накладные расходы** | Нет (embedded‑режим) | TCP (0,1–0,2 мс) |
| **Задержка чтения** | ~150 нс | ~0,24–0,31 мс |
| **Задержка записи** | ~1 070 нс | ~0,45 мс |
| **Персистентность** | Полная, с fsync | Опциональная (RDB/AOF) |
| **Транзакции** | ACID, Snapshot Isolation | Нет (только pipelining) |
| **MVCC** | Да | Нет |
| **Column Families** | Да | Нет |
| **Встраиваемость** | Да, `import` | Нет |

**Итог:** нужен быстрый кеш? Берите Redis. Нужна надёжная встраиваемая база с удалённым доступом? Попробуйте ScoriaDB.

---

## 🧩 Возможности и функции

### LSM‑движок

| Компонент | Статус |
|:---|:---:|
| MemTable (B‑tree) | ✅ |
| SSTable (блочный индекс, Bloom, префиксное сжатие) | ✅ |
| Leveled Compaction | ✅ |
| Value Log (WiscKey, >64 байт) | ✅ |
| Сжатие (Snappy, Zstd) | ✅ |

### Надёжность и журналы

| Компонент | Статус |
|:---|:---:|
| WAL + fsync + восстановление | ✅ |
| Manifest + fsync | ✅ |
| CRC32 блоков | ✅ |
| Fail‑safe VLog | ✅ |

### Транзакции и MVCC

| Возможность | Статус |
|:---|:---:|
| MVCC, Snapshot Isolation | ✅ |
| Интерактивные транзакции | ✅ |
| WriteBatch | ✅ |
| Обнаружение конфликтов | ✅ |

### Column Families

| Возможность | Статус |
|:---|:---:|
| Независимые LSM‑деревья | ✅ |
| Общий WAL (атомарность между CF) | ✅ |

### API и интерфейсы

| Интерфейс | Статус |
|:---|:---:|
| Embedded Go API | ✅ |
| gRPC | ✅ |
| REST | ✅ |
| WebSocket | ✅ |
| CLI (Cobra, интерактивный) | ✅ |

### Безопасность и мониторинг

| Возможность | Статус |
|:---|:---:|
| JWT‑аутентификация | ✅ |
| Роли (admin, readwrite, readonly) | ✅ |
| Начальный пользователь (`admin/admin`) | ✅ |
| Метрики Prometheus | ✅ |
| Эндпоинты health / ready | ✅ |

---

## 🛡️ Надёжность и восстановление

ScoriaDB спроектирована так, чтобы **не терять данные** даже при внезапном отключении питания:

1. **WAL** – каждая операция пишется с CRC32 и `fsync` перед попаданием в MemTable. При сбое WAL воспроизводится.
2. **Manifest** – журнал метаданных с `fsync`. При старте точный набор файлов восстанавливается.
3. **Value Log** – при несовпадении magic файл переименовывается в `.corrupt`, создаётся новый, данные восстанавливаются из WAL.

**Цена:** `fsync` на каждый WriteBatch примерно в 5 раз медленнее буферизованной записи. Group Commit появится в v0.2.0.

---

## 🕰️ Как работает MVCC

- Каждая операция `Put` создаёт новую версию с временной меткой `commitTS`.
- Транзакция при `Begin()` получает `startTS` — снимок базы.
- Чтения видят версии с `commitTS ≤ startTS`.
- При `Commit()`: если какой‑либо изменённый ключ имеет более новую версию (`commitTS > startTS`) → `ErrConflict`. Нужно повторить транзакцию.
- **Писатели никогда не блокируют читателей.**

**Инвертированная метка:** хранится `^commitTS`, чтобы свежие записи оказывались первыми в итераторе.

```go
db.Put("user:1", "alice")   // commitTS = 100
db.Put("user:1", "bob")     // commitTS = 101
// Итератор покажет сначала bob, потом alice.
```

---

## 📚 Документация

Полная документация находится в папке [`docs/`](docs/):

| Язык | Документация | Пример |
|:---|:---|:---|
| **Go (встраиваемый)** | [Go-Embedded](docs/README.md#go-embedded-api) | [pkg/scoria](pkg/scoria/) |
| **Python** | [python-doc.md](docs/python/python-doc.md) | [example.py](docs/python/example.py) |
| **Java** | [java-doc.md](docs/java/java-doc.md) | [example.java](docs/java/example.java) |
| **C++** | [cpp-doc.md](docs/c++/cpp-doc.md) | [example.cpp](docs/c++/example.cpp) |

**Быстрый старт через Docker:**

```bash
git clone https://github.com/f4ga/ScoriaDB.git
cd ScoriaDB
docker compose -f deployments/docker-compose.yml up --build
docker exec -it scoria-server ./scoria-cli admin auth admin admin
docker exec -it scoria-server ./scoria-cli --token <токен> set hello world
```

**Локальная сборка:**

```bash
go build -o scoria-server ./cmd/server
go build -o scoria-cli ./cmd/cli
./scoria-server &
TOKEN=$(./scoria-cli admin auth admin admin)
./scoria-cli --token "$TOKEN" set hello world
```

**Встраиваемый Go API:**

```go
import "github.com/f4ga/ScoriaDB/pkg/scoria"
db, _ := scoria.NewScoriaDB("./data")
defer db.Close()
db.Put([]byte("hello"), []byte("world"))
```

---

## 📈 Критерии выпуска v0.1.0

Все пункты выполнены ✅ :

| Категория | Статус |
|:---|:---:|
| LSM-движок, SSTable, компакшн | ✅ |
| MVCC, Snapshot Isolation | ✅ |
| Транзакции, WriteBatch | ✅ |
| Column Families | ✅ |
| Embedded Go, gRPC, REST, CLI | ✅ |
| JWT, роли | ✅ |
| Docker, CI/CD | ✅ |
| Метрики Prometheus, health | ✅ |
| Модульные, интеграционные, стресс-тесты | ✅ |

---

## 📁 Структура проекта

```
scoriadb/
├── cmd/                     # Точки входа (сервер, CLI)
├── internal/                # Ядро (движок, MVCC, транзакции, CF, API)
├── pkg/scoria/              # Публичное API для встраивания
├── proto/                   # Protobuf спецификации
├── tests/                   # E2E и стресс-тесты
├── deployments/             # Docker
└── docs/                    # Документация (Python, Java, C++ гайды)
```

---

## 🗺️ Дорожная карта версий

| Версия | Что будет |
|:---|:---|
| **v0.1.0** | ✅ Стабильное ядро, gRPC, CLI, Docker, тесты |
| **v0.2.0** | Web UI, Group Commit, TTL, бинарный Manifest, исправление VFS |
| **v0.3.0** | Lock‑free skip list, настоящий zero‑copy, автоматический GC |
| **v1.0.0** | Репликация Raft, шардирование, распределённые транзакции, Sorted Sets |

---

## 📄 Лицензия

**Apache License 2.0** — подробности в файле [LICENSE](LICENSE).

Разрешается: использовать, модифицировать, распространять, сублицензировать, использовать в коммерческих проектах.  
Запрещается: использовать название ScoriaDB.

---

## ❓ FAQ

**ScoriaDB готова к продакшену?**  
v0.1.0 — для умеренных нагрузок (10–20 конкурентных записей). Для 1000+ горутин лучше подождать v0.3.0 (lock‑free MemTable, автоматический GC).

**Zero‑copy реально работает?**  
Нет. Текущая реализация копирует из mmap для безопасности. Настоящий zero‑copy будет в v0.3.0.

**Почему запись тормозит?**  
Из-за `fsync` на каждый батч. Group Commit в v0.2.0 снизит задержки в 3–5 раз.

**Можно ли использовать из Python?**  
Да, через gRPC — см. [docs/python/](docs/python/).

**Могу ли я внести вклад?**  
Да! Нужна помощь с автоматическим GC, lock‑free структурами, тестированием на Windows/macOS и Web UI.

---

## 🤝 Поддержать проект

- ⭐ **Поставьте звезду** на GitHub
- 🐛 **Сообщите об ошибке** через Issues
- 💻 **Отправьте Pull Request**
- 📣 **Расскажите о проекте**

---

<div align="center">
  <i>Крепкая как камень. Лёгкая как пепел.</i>
  <br><br>
  <a href="https://github.com/f4ga/ScoriaDB">github.com/f4ga/ScoriaDB</a>
  <br><br>
  <a href="docs/README.md"><img src="https://img.shields.io/badge/📖-Полная%20документация-blue?style=for-the-badge" alt="Документация"></a>
  <br><br>
  <img src="https://capsule-render.vercel.app/api?type=waving&color=gradient&customColorList=12&height=120&section=footer">
</div>