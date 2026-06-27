// Copyright 2026 Ekaterina Godulyan
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package engine

import (
	"bytes"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/f4ga/ScoriaDB/internal/keys"
	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// ============================================================
// EBR (Epoch-Based Reclamation)
// ============================================================

const (
	maxEpochs  = 3
	maxRetired = 1024
)

// EpochManager manages epochs for safe memory reclamation.
type EpochManager struct {
	globalEpoch atomic.Int64
	threads     sync.Map // goroutine ID → epoch
	retired     [maxEpochs][]unsafe.Pointer
	mu          sync.Mutex
}

var epochManager = &EpochManager{
	retired: [maxEpochs][]unsafe.Pointer{},
}

// getGoroutineID returns the current goroutine ID by parsing runtime.Stack.
func getGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	var id uint64
	for i := 0; i < n; i++ {
		if buf[i] == ' ' {
			for j := i + 1; j < n; j++ {
				if buf[j] >= '0' && buf[j] <= '9' {
					id = id*10 + uint64(buf[j]-'0')
				} else {
					return id
				}
			}
			return id
		}
	}
	return 0
}

// EnterEpoch marks the goroutine as active in the current epoch.
func (em *EpochManager) EnterEpoch() int64 {
	id := getGoroutineID()
	epoch := em.globalEpoch.Load()
	em.threads.Store(id, epoch)
	return epoch
}

// ExitEpoch removes the goroutine from the active set.
func (em *EpochManager) ExitEpoch() {
	id := getGoroutineID()
	em.threads.Delete(id)
}

// Retire marks a pointer for safe reclamation.
func (em *EpochManager) Retire(ptr unsafe.Pointer) {
	em.mu.Lock()
	defer em.mu.Unlock()

	epoch := em.globalEpoch.Load() % maxEpochs
	em.retired[epoch] = append(em.retired[epoch], ptr)

	if len(em.retired[epoch]) > maxRetired {
		em.Clean()
	}
}

// Clean reclaims retired pointers that are no longer protected.
func (em *EpochManager) Clean() {
	em.mu.Lock()
	defer em.mu.Unlock()

	activeEpochs := make(map[int64]struct{})
	em.threads.Range(func(_, value interface{}) bool {
		if epoch, ok := value.(int64); ok {
			activeEpochs[epoch] = struct{}{}
		}
		return true
	})

	for epoch := 0; epoch < maxEpochs; epoch++ {
		if _, active := activeEpochs[int64(epoch)]; active {
			continue
		}
		for _, ptr := range em.retired[epoch] {
			_ = (*Node)(ptr) // allow GC
		}
		em.retired[epoch] = nil
	}
}

// AdvanceEpoch advances the global epoch and triggers cleanup.
func (em *EpochManager) AdvanceEpoch() {
	em.globalEpoch.Add(1)
	em.Clean()
}

// ============================================================
// Skip List
// ============================================================

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

const (
	maxHeight = 32
	threshold = 1073741824 // 0.25 * 0xFFFFFFFF
)

func randomHeight() int {
	h := 1
	for h < maxHeight && fastrand() < threshold {
		h++
	}
	return h
}

// Node is a skip list node with lock-free fields.
type Node struct {
	key     mvcc.MVCCKey
	value   atomic.Pointer[[]byte]
	deleted atomic.Bool
	next    []atomic.Pointer[Node]
	height  int
}

func newNode(key mvcc.MVCCKey, value []byte, height int) *Node {
	n := &Node{
		key:    key,
		height: height,
		next:   make([]atomic.Pointer[Node], height),
	}
	if value != nil {
		n.value.Store(&value)
	}
	return n
}

func nodeKeyLess(a, b mvcc.MVCCKey) bool {
	cmp := keys.CompareKeys(a.Key, b.Key)
	if cmp < 0 {
		return true
	}
	if cmp > 0 {
		return false
	}
	return a.Timestamp > b.Timestamp
}

// SkipList is a lock-free concurrent skip list with EBR.
type SkipList struct {
	head   *Node
	height int32
	length int64
}

// NewSkipList creates a new empty skip list.
func NewSkipList() *SkipList {
	return &SkipList{
		head:   newNode(mvcc.MVCCKey{}, nil, maxHeight),
		height: 1,
	}
}

// findRaw searches for the key and returns predecessors and successors at all levels.
func (s *SkipList) findRaw(key mvcc.MVCCKey) ([]*Node, []*Node) {
	prevs := make([]*Node, maxHeight)
	nexts := make([]*Node, maxHeight)

	for level := 0; level < maxHeight; level++ {
		prevs[level] = s.head
	}

	current := s.head
	for level := atomic.LoadInt32(&s.height) - 1; level >= 0; level-- {
		for {
			next := current.next[level].Load()
			if next == nil || !nodeKeyLess(next.key, key) {
				break
			}
			current = next
		}
		prevs[level] = current
		nexts[level] = current.next[level].Load()
	}
	return prevs, nexts
}

// Get returns the value for a key.
func (s *SkipList) Get(key mvcc.MVCCKey) ([]byte, bool) {
	epochManager.EnterEpoch()
	defer epochManager.ExitEpoch()

	_, nexts := s.findRaw(key)

	node := nexts[0]
	if node == nil || node.key.Key == nil || !bytes.Equal(node.key.Key, key.Key) {
		return nil, false
	}
	if node.deleted.Load() {
		return nil, false
	}
	val := node.value.Load()
	if val == nil {
		return nil, false
	}
	return *val, true
}

// Put inserts or updates a key-value pair.
func (s *SkipList) Put(key mvcc.MVCCKey, value []byte) {
	epochManager.EnterEpoch()
	defer epochManager.ExitEpoch()

	height := randomHeight()

	for {
		prevs, nexts := s.findRaw(key)

		// If the key exists, update the value.
		if nexts[0] != nil && bytes.Equal(nexts[0].key.Key, key.Key) && nexts[0].key.Timestamp == key.Timestamp {
			node := nexts[0]
			if node.deleted.Load() {
				node.deleted.Store(false)
			}
			for {
				old := node.value.Load()
				if node.value.CompareAndSwap(old, &value) {
					return
				}
			}
		}

		node := newNode(key, value, height)

		for level := 0; level < height; level++ {
			node.next[level].Store(nexts[level])
		}

		// Top-down CAS: start from the highest level.
		success := true
		for level := height - 1; level >= 0; level-- {
			if !prevs[level].next[level].CompareAndSwap(nexts[level], node) {
				success = false
				break
			}
		}

		if !success {
			continue
		}

		if height > int(atomic.LoadInt32(&s.height)) {
			for height > int(atomic.LoadInt32(&s.height)) {
				atomic.CompareAndSwapInt32(&s.height, atomic.LoadInt32(&s.height), int32(height))
			}
		}

		atomic.AddInt64(&s.length, 1)
		return
	}
}

// Delete marks a key as deleted.
func (s *SkipList) Delete(key mvcc.MVCCKey) bool {
	epochManager.EnterEpoch()
	defer epochManager.ExitEpoch()

	for {
		_, nexts := s.findRaw(key)

		if nexts[0] == nil || !bytes.Equal(nexts[0].key.Key, key.Key) {
			return false
		}
		node := nexts[0]

		if !node.deleted.CompareAndSwap(false, true) {
			continue
		}

		atomic.AddInt64(&s.length, -1)
		epochManager.Retire(unsafe.Pointer(node))
		return true
	}
}

// Len returns the number of elements in the list.
func (s *SkipList) Len() int {
	return int(atomic.LoadInt64(&s.length))
}

// Height returns the current maximum height.
func (s *SkipList) Height() int {
	return int(atomic.LoadInt32(&s.height))
}

// ============================================================
// Iterator
// ============================================================

// SkipListIterator iterates over the skip list.
type SkipListIterator struct {
	current *Node
	list    *SkipList
	done    bool
}

// NewIterator creates a new iterator.
func (s *SkipList) NewIterator() *SkipListIterator {
	return &SkipListIterator{
		current: s.head,
		list:    s,
	}
}

// Next advances the iterator.
func (it *SkipListIterator) Next() bool {
	if it.done {
		return false
	}
	for {
		next := it.current.next[0].Load()
		if next == nil {
			it.done = true
			return false
		}
		it.current = next
		if !next.deleted.Load() {
			return true
		}
	}
}

// Key returns the current key.
func (it *SkipListIterator) Key() mvcc.MVCCKey {
	return it.current.key
}

// Value returns the current value.
func (it *SkipListIterator) Value() []byte {
	val := it.current.value.Load()
	if val == nil {
		return nil
	}
	return *val
}

// Close releases the iterator.
func (it *SkipListIterator) Close() {
	it.current = nil
	it.list = nil
	it.done = true
}
