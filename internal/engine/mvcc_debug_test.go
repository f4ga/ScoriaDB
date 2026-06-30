package engine

import (
	"testing"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// TestMVCC_Tombstone_Debug instruments the exact scenario that fails:
// tombstone at commitTS=20, then query at snapshotTS=25 (should be deleted)
// and snapshotTS=15 (should see v1).
func TestMVCC_Tombstone_Debug(t *testing.T) {
	mt := NewMemTable()

	key := []byte("testkey")

	// Step 1: Insert v1 at commitTS=10
	mvccKey1 := mvcc.NewMVCCKey(key, 10)
	mt.Put(mvccKey1, []byte("value_v1"))
	t.Logf("[SETUP] Inserted v1 at commitTS=10 (inverted TS=%d)", mvccKey1.Timestamp)

	// Step 2: Insert tombstone at commitTS=20
	mvccKey2 := mvcc.NewMVCCKey(key, 20)
	mt.DeleteWithTS(mvccKey2)
	t.Logf("[SETUP] Inserted tombstone at commitTS=20 (inverted TS=%d)", mvccKey2.Timestamp)

	// Step 3: Query at snapshotTS=25 — should return (nil, false) because
	// the tombstone at TS=20 is visible (20 <= 25).
	queryKey25 := mvcc.NewMVCCKey(key, 25)
	val25, found25 := mt.Get(queryKey25)
	t.Logf("[DEBUG] Query TS=25: Found? %v, Value= %q.", found25, string(val25))

	// Step 4: Query at snapshotTS=15 — should return ("value_v1", true)
	// because v1 at TS=10 is visible (10 <= 15) and tombstone at TS=20 is not.
	queryKey15 := mvcc.NewMVCCKey(key, 15)
	val15, found15 := mt.Get(queryKey15)
	t.Logf("[DEBUG] Query TS=15: Found? %v, Value= %q.", found15, string(val15))

	// Step 5: Dump the entire level-0 chain for this key
	t.Logf("[DEBUG] Node chain for key %q:", key)
	searchKey := mvcc.MVCCKey{Key: key, Timestamp: mvcc.InvertTimestamp(0)}
	node := mt.sl.findGreaterOrEqual(searchKey)
	for node != nil {
		nk := node.Key()
		if string(nk.Key) != string(key) {
			break
		}
		commitTS := nk.CommitTS()
		t.Logf("[DEBUG]   (TS=%d, Deleted=%v, Value=%q)", commitTS, node.deleted.Load(), string(node.Value()))
		node = node.next[0].Load()
	}

	// Step 6: Assertions to catch the bug
	if found25 {
		t.Errorf("[FAIL] snapshotTS=25: expected NOT FOUND (tombstone at TS=20), but got value=%q", string(val25))
	} else {
		t.Logf("[PASS] snapshotTS=25: correctly returns NOT FOUND")
	}

	if !found15 {
		t.Errorf("[FAIL] snapshotTS=15: expected FOUND (v1 at TS=10), but got NOT FOUND")
	} else if string(val15) != "value_v1" {
		t.Errorf("[FAIL] snapshotTS=15: expected value=%q, got %q", "value_v1", string(val15))
	} else {
		t.Logf("[PASS] snapshotTS=15: correctly returns value=%q", string(val15))
	}
}
