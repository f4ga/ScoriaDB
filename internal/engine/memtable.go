package engine

import (
	"bytes"
	"sync"

	"github.com/google/btree"
	"scoriadb/internal/mvcc"
)

// MemTable is implemented using B-tree (google/btree) as a learning compromise.
// B-tree is used for simplicity; a Skiplist can be evaluated in the future.

// mvccEntry represents an entry in MemTable.
type mvccEntry struct {
	Key     mvcc.MVCCKey
	Value   []byte
	Deleted bool // true for tombstone (deletion)
}

// Less for btree.Item.
func (e mvccEntry) Less(than btree.Item) bool {
	return e.Key.Less(than.(mvccEntry).Key)
}

// MemTable is a thread‑safe in‑memory structure based on btree.
type MemTable struct {
	mu   sync.RWMutex
	tree *btree.BTree
	size int // number of elements
}

// NewMemTable creates a new MemTable.
func NewMemTable() *MemTable {
	return &MemTable{
		tree: btree.New(32), // branching factor
	}
}

// Put adds or updates a key with the given timestamp.
func (mt *MemTable) Put(key mvcc.MVCCKey, value []byte) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	entry := mvccEntry{Key: key, Value: value, Deleted: false}
	// If a key with the same timestamp already exists, replace it
	if mt.tree.Has(entry) {
		mt.tree.Delete(entry)
	} else {
		mt.size++
	}
	mt.tree.ReplaceOrInsert(entry)
}

// DeleteWithTS marks a key as deleted (tombstone) at the given commit timestamp.
// Internally creates an entry with Deleted = true and empty value.
func (mt *MemTable) DeleteWithTS(key mvcc.MVCCKey) {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	entry := mvccEntry{Key: key, Value: nil, Deleted: true}
	// If a key with the same timestamp already exists, replace it
	if mt.tree.Has(entry) {
		mt.tree.Delete(entry)
	} else {
		mt.size++
	}
	mt.tree.ReplaceOrInsert(entry)
}

// Get returns the value for the key with the maximum commit timestamp <= snapshotTS.
// If the key is not found, returns nil.
//
// # MVCC logic and timestamp inversion
//
// ScoriaDB uses an approach similar to TiKV and BadgerDB:
//   - Each key version is encoded as an MVCCKey, consisting of UserKey and inverted timestamp.
//   - Inverted timestamp = math.MaxUint64 - commitTS. This guarantees that in sorted order
//     newer versions (with larger commitTS) appear before older ones (since they have a smaller
//     inverted timestamp). For details:
//     * TiKV: “Keys and timestamps are encoded so that timestamped keys are sorted first by key
//       (ascending), then by timestamp (descending)” (https://pkg.go.dev/github.com/pingcap-incubator/tinykv/kv/transaction/mvcc)
//     * BadgerDB: “Badger uses Multi-Version Concurrency Control (MVCC)” (https://godocs.io/github.com/dgraph-io/badger)
//
// # Visibility formula
//
// A transaction with snapshotTS sees a version if commitTS <= snapshotTS.
// Let T(x) = MaxUint64 - x be the inverted timestamp.
// Then condition commitTS <= snapshotTS is equivalent to T(commitTS) >= T(snapshotTS).
//
// # Search algorithm (corrected)
//
// Due to sorting order (newer versions come before older) using AscendGreaterOrEqual
// would start iteration from the oldest visible version, not the newest.
// To correctly find the newest visible version we must use DescendLessOrEqual,
// which starts traversal from the entry closest to searchKey and moves downwards
// (from newer to older). This matches industrial implementations (TiKV, BadgerDB) where
// search is performed in reverse timestamp order.
//
// Algorithm:
// 1. Search key (key) contains inverted snapshotTS: Timestamp = T(snapshotTS).
// 2. In B‑Tree entries are sorted first by UserKey (ascending), then by inverted
//    timestamp (descending), meaning: for the same UserKey newer versions (with larger commitTS)
//    appear before older ones, because they have a smaller inverted timestamp.
// 3. Use DescendLessOrEqual, which walks entries in reverse sort order
//    (from newer to older), starting from a version that is <= searchKey.
// 4. Visibility condition: commitTS <= snapshotTS  <=>  inverted timestamp >= key.Timestamp.
// 5. Look for the newest version satisfying the condition, i.e. the first entry with the same UserKey
//    where entry.Key.Timestamp >= key.Timestamp.
//
// # Tombstone handling (deletion)
//
// In ScoriaDB deletion is modeled by an entry with Deleted = true (tombstone).
// Semantics match industrial MVCC systems (TiKV, BadgerDB):
//   - If for a given snapshotTS the visible version is a tombstone (commitTS <= snapshotTS),
//     the key is considered deleted and must not be found (returns found = false).
//   - Tombstone hides all older versions for snapshotTS >= deletion commitTS.
//   - If tombstone is not visible (commitTS > snapshotTS), it is ignored and search continues
//     for older versions.
//
// Implementation:
//   - When a visible version with Deleted = true is encountered, iteration stops, returns
//     found = false.
//   - When a visible live version is encountered, returns its value and found = true.
//   - If a version is too new (commitTS > snapshotTS), iteration continues.
//
// Detailed explanation:
// - DescendLessOrEqual guarantees we start from a version that is <= searchKey in sort order.
// - Because newer versions have smaller inverted timestamp, they will be “less” in sort order,
//   and DescendLessOrEqual will start from the newest version that does not exceed searchKey.
// - If that version is too new (commitTS > snapshotTS), we continue iteration (move to older ones).
// - If the version is visible (commitTS <= snapshotTS), we stop because it is the newest visible version.
//
// References:
// - TiKV: https://cloud.tencent.com/developer/article/1549435 (lines 29-31)
// - BadgerDB: https://godocs.io/github.com/dgraph-io/badger
func (mt *MemTable) Get(key mvcc.MVCCKey) ([]byte, bool) {
	mt.mu.RLock()
	defer mt.mu.RUnlock()

	var candidate []byte
	found := false
	mt.tree.DescendLessOrEqual(mvccEntry{Key: key}, func(item btree.Item) bool {
		entry := item.(mvccEntry)
		// If the key differs, we have moved beyond the UserKey of interest.
		// Since entries are sorted first by UserKey, all subsequent entries will have a different UserKey.
		// Stop iteration.
		if !bytes.Equal(entry.Key.Key, key.Key) {
			return false
		}
		// Check visibility condition: commitTS <= snapshotTS
		// which is equivalent to entry.Key.Timestamp >= key.Timestamp
		if entry.Key.Timestamp >= key.Timestamp {
			// Version is visible.
			if entry.Deleted {
				// Tombstone: key is deleted for this snapshot.
				// Stop search because tombstone hides older versions.
				// found remains false.
				return false
			}
			// Live version.
			candidate = entry.Value
			found = true
			return false
		}
		// entry.Key.Timestamp < key.Timestamp => commitTS > snapshotTS, version too new.
		// Continue iteration to move to older versions (which may be visible).
		return true
	})

	return candidate, found
}

// NewIterator returns an iterator over all entries in MemTable.
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

// Size returns the number of elements in MemTable.
func (mt *MemTable) Size() int {
	mt.mu.RLock()
	defer mt.mu.RUnlock()
	return mt.size
}

// MemTableIterator iterates over MemTable entries.
type MemTableIterator struct {
	entries []mvccEntry
	index   int
}

// Next moves the iterator to the next entry.
func (it *MemTableIterator) Next() bool {
	it.index++
	return it.index < len(it.entries)
}

// Key returns the current key.
func (it *MemTableIterator) Key() mvcc.MVCCKey {
	return it.entries[it.index].Key
}

// Value returns the current value.
func (it *MemTableIterator) Value() []byte {
	return it.entries[it.index].Value
}

// Close releases iterator resources.
func (it *MemTableIterator) Close() {
	it.entries = nil
}