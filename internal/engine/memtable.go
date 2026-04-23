package engine

import (
	"bytes"
	"sync"

	"github.com/google/btree"
	"scoriadb/internal/mvcc"
)

// MemTable реализован на основе B-Tree (google/btree) в качестве учебного компромисса.
// В промышленных LSM-движках (LevelDB, RocksDB, Badger) MemTable обычно реализуется
// на основе Skiplist по следующим причинам:
//  1. Простота конкурентного доступа (lock-free или тонкие блокировки)
//  2. Отсутствие дорогой балансировки при вставке
//  3. Эффективный flush на диск (естественно отсортированный порядок)
//  4. Гибкость по памяти (узлы переменного размера)
//
// B-Tree выбран для ScoriaDB потому что:
//  - Готовый, надёжный пакет (github.com/google/btree)
//  - Copy-on-write семантика (дешёвые клоны через btree.Clone())
//  - Удобство для учебного/демонстрационного проекта
//  - Более знаком разработчикам из SQL-мира
//
// Для production-рекомендации следует рассмотреть замену на Skiplist.

// mvccEntry представляет запись в MemTable.
type mvccEntry struct {
	Key     mvcc.MVCCKey
	Value   []byte
	Deleted bool // true для tombstone (удаление)
}

// Less для btree.Item.
func (e mvccEntry) Less(than btree.Item) bool {
	return e.Key.Less(than.(mvccEntry).Key)
}

// MemTable потокобезопасная in-memory структура на основе btree.
type MemTable struct {
	mu   sync.RWMutex
	tree *btree.BTree
	size int // количество элементов
}

// NewMemTable создает новую MemTable.
func NewMemTable() *MemTable {
	return &MemTable{
		tree: btree.New(32), // степень ветвления
	}
}

// Put добавляет или обновляет ключ с указанным timestamp.
func (mt *MemTable) Put(key mvcc.MVCCKey, value []byte) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	entry := mvccEntry{Key: key, Value: value, Deleted: false}
	// Если ключ с таким же timestamp уже существует, заменяем
	if mt.tree.Has(entry) {
		mt.tree.Delete(entry)
	} else {
		mt.size++
	}
	mt.tree.ReplaceOrInsert(entry)
}

// DeleteWithTS помечает ключ как удалённый (tombstone) на указанный commit timestamp.
// Внутренне создаётся запись с Deleted = true и пустым значением.
func (mt *MemTable) DeleteWithTS(key mvcc.MVCCKey) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	entry := mvccEntry{Key: key, Value: nil, Deleted: true}
	// Если ключ с таким же timestamp уже существует, заменяем
	if mt.tree.Has(entry) {
		mt.tree.Delete(entry)
	} else {
		mt.size++
	}
	mt.tree.ReplaceOrInsert(entry)
}

// Get возвращает значение для ключа с максимальным commit timestamp <= snapshotTS.
// Если ключ не найден, возвращает nil.
//
// # Логика MVCC и инвертирование timestamp
//
// В ScoriaDB используется подход, аналогичный TiKV и BadgerDB:
//   - Каждая версия ключа кодируется как MVCCKey, состоящий из UserKey и инвертированного timestamp.
//   - Инвертированный timestamp = math.MaxUint64 - commitTS. Это гарантирует, что в отсортированном
//     порядке более новые версии (с большим commitTS) располагаются перед старыми (поскольку у них
//     меньший инвертированный timestamp). Подробнее:
//     * TiKV: «Keys and timestamps are encoded so that timestamped keys are sorted first by key
//       (ascending), then by timestamp (descending)» (https://pkg.go.dev/github.com/pingcap-incubator/tinykv/kv/transaction/mvcc)
//     * BadgerDB: «Badger uses Multi-Version Concurrency Control (MVCC)» (https://godocs.io/github.com/dgraph-io/badger)
//
// # Формула видимости
//
// Транзакция с snapshotTS видит версию, если commitTS <= snapshotTS.
// Пусть T(x) = MaxUint64 - x — инвертированный timestamp.
// Тогда условие commitTS <= snapshotTS эквивалентно T(commitTS) >= T(snapshotTS).
//
// # Алгоритм поиска (исправленный)
//
// Из-за порядка сортировки (новые версии идут перед старыми) использование AscendGreaterOrEqual
// приводит к тому, что итерация начинается с самой старой видимой версии, а не с самой новой.
// Для корректного поиска самой новой видимой версии необходимо использовать DescendLessOrEqual,
// который начинает обход с версии, наиболее близкой к searchKey, и движется в сторону убывания
// (от новых к старым). Это соответствует промышленным реализациям (TiKV, BadgerDB), где поиск
// выполняется в обратном порядке по timestamp.
//
// Алгоритм:
// 1. Ключ поиска (key) содержит инвертированный snapshotTS: Timestamp = T(snapshotTS).
// 2. В B‑Tree записи отсортированы сначала по UserKey (возрастание), затем по инвертированному
//    timestamp (убывание), что означает: для одного UserKey более новые версии (с большим commitTS)
//    идут раньше старых, потому что у них меньший инвертированный timestamp.
// 3. Используем DescendLessOrEqual, который проходит записи в обратном порядке сортировки
//    (от новых к старым), начиная с версии, которая <= searchKey.
// 4. Условие видимости: commitTS <= snapshotTS  <=>  инвертированный timestamp >= key.Timestamp.
// 5. Ищем самую новую версию, удовлетворяющую условию, т.е. первую запись с тем же UserKey,
//    у которой entry.Key.Timestamp >= key.Timestamp.
//
// # Обработка tombstone (удаление)
//
// В ScoriaDB удаление моделируется записью с флагом Deleted = true (tombstone).
// Семантика соответствует промышленным MVCC-системам (TiKV, BadgerDB):
//   - Если для данного snapshotTS видимая версия является tombstone (commitTS <= snapshotTS),
//     ключ считается удалённым и не должен быть найден (возвращается found = false).
//   - Tombstone скрывает все более старые версии для snapshotTS >= commitTS удаления.
//   - Если tombstone не видна (commitTS > snapshotTS), она игнорируется, и поиск продолжается
//     для более старых версий.
//
// Реализация:
//   - При обнаружении видимой версии с Deleted = true итерация останавливается, возвращается
//     found = false.
//   - При обнаружении видимой живой версии возвращается её значение и found = true.
//   - Если видимая версия слишком новая (commitTS > snapshotTS), итерация продолжается.
//
// Подробное объяснение:
// - DescendLessOrEqual гарантирует, что мы начинаем с версии, которая <= searchKey по порядку сортировки.
// - Поскольку новые версии имеют меньший инвертированный timestamp, они будут "меньше" в смысле сортировки,
//   и DescendLessOrEqual начнёт с самой новой версии, которая не превышает searchKey.
// - Если эта версия слишком новая (commitTS > snapshotTS), мы продолжаем итерацию (двигаемся к более старым).
// - Если версия видима (commitTS <= snapshotTS), мы останавливаемся, потому что это самая новая видимая версия.
//
// Ссылки:
// - TiKV: https://cloud.tencent.com/developer/article/1549435 (строки 29-31)
// - BadgerDB: https://godocs.io/github.com/dgraph-io/badger
func (mt *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	var candidate []byte
	found := false
	mt.tree.DescendLessOrEqual(mvccEntry{Key: key}, func(item btree.Item) bool {
		entry := item.(mvccEntry)
		// Если ключ отличается, значит мы вышли за пределы интересующего нас UserKey.
		// Поскольку записи отсортированы сначала по UserKey, все последующие записи будут иметь другой UserKey.
		// Останавливаем итерацию.
		if !bytes.Equal(entry.Key.Key, key.Key) {
			return false
		}
		// Проверяем условие видимости: commitTS <= snapshotTS
		// что эквивалентно entry.Key.Timestamp >= key.Timestamp
		if entry.Key.Timestamp >= key.Timestamp {
			// Версия видима.
			if entry.Deleted {
				// Tombstone: ключ удалён для этого snapshot.
				// Останавливаем поиск, так как tombstone скрывает более старые версии.
				// found остаётся false.
				return false
			}
			// Живая версия.
			candidate = entry.Value
			found = true
			return false
		}
		// entry.Key.Timestamp < key.Timestamp => commitTS > snapshotTS, версия слишком новая.
		// Продолжаем итерацию, чтобы перейти к более старым версиям (которые могут быть видимы).
		return true
	})

	return candidate, found
}

// NewIterator возвращает итератор по всем записям в MemTable.
func (mt *MemTable) NewIterator() *MemTableIterator {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	var entries []mvccEntry
	mt.tree.Ascend(func(item btree.Item) bool {
		entries = append(entries, item.(mvccEntry))
		return true
	})
	return &MemTableIterator{
		entries: entries,
		index:   -1,
	}
}

// Size возвращает количество элементов в MemTable.
func (mt *MemTable) Size() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.size
}

// MemTableIterator итератор по записям MemTable.
type MemTableIterator struct {
	entries []mvccEntry
	index   int
}

// Next перемещает итератор к следующей записи.
func (it *MemTableIterator) Next() bool {
	it.index++
	return it.index < len(it.entries)
}

// Key возвращает текущий ключ.
func (it *MemTableIterator) Key() mvcc.MVCCKey {
	return it.entries[it.index].Key
}

// Value возвращает текущее значение.
func (it *MemTableIterator) Value() []byte {
	return it.entries[it.index].Value
}

// Close освобождает ресурсы итератора.
func (it *MemTableIterator) Close() {
	it.entries = nil
}