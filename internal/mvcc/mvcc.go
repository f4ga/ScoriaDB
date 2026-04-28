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

package mvcc

import (
	"bytes"
	"math"
	"sync/atomic"

	"github.com/google/btree"
)

// TimestampGenerator генерирует монотонные временные метки.
type TimestampGenerator struct {
	counter uint64 // atomic
}

// NewTimestampGenerator создает новый генератор.
func NewTimestampGenerator() *TimestampGenerator {
	return &TimestampGenerator{counter: 1} // начинаем с 1, 0 может быть специальным значением
}

// Next возвращает следующий timestamp.
func (tg *TimestampGenerator) Next() uint64 {
	return atomic.LoadUint64(&tg.counter)
}

// Increment увеличивает счетчик и возвращает новый timestamp.
func (tg *TimestampGenerator) Increment() uint64 {
	return atomic.AddUint64(&tg.counter, 1)
}

// Set устанавливает счетчик в значение (для восстановления).
func (tg *TimestampGenerator) Set(val uint64) {
	for {
		old := atomic.LoadUint64(&tg.counter)
		if val <= old {
			return
		}
		if atomic.CompareAndSwapUint64(&tg.counter, old, val) {
			return
		}
	}
}

// InvertTimestamp инвертирует timestamp для сортировки новых версий первыми.
// Используется в MVCCKey.
func InvertTimestamp(ts uint64) uint64 {
	return math.MaxUint64 - ts
}

// RevertTimestamp восстанавливает оригинальный timestamp из инвертированного.
func RevertTimestamp(inverted uint64) uint64 {
	return math.MaxUint64 - inverted
}

// MVCCKey представляет ключ с версией (инвертированный timestamp).
type MVCCKey struct {
	Key       []byte
	Timestamp uint64 // инвертированный: MaxUint64 - commitTS
}

// NewMVCCKey создает MVCCKey из пользовательского ключа и commit timestamp.
func NewMVCCKey(key []byte, commitTS uint64) MVCCKey {
	return MVCCKey{
		Key:       key,
		Timestamp: InvertTimestamp(commitTS),
	}
}

// CommitTS возвращает оригинальный commit timestamp.
func (k MVCCKey) CommitTS() uint64 {
	return RevertTimestamp(k.Timestamp)
}

// Compare сравнивает два ключа лексикографически (ключ, затем timestamp).
func (k MVCCKey) Compare(other MVCCKey) int {
	// Сравниваем ключи
	if cmp := bytesCompare(k.Key, other.Key); cmp != 0 {
		return cmp
	}
	// Более новые версии (больший инвертированный timestamp) идут первыми
	if k.Timestamp > other.Timestamp {
		return -1
	}
	if k.Timestamp < other.Timestamp {
		return 1
	}
	return 0
}

// Less реализует интерфейс btree.Item для сортировки.
func (k MVCCKey) Less(than btree.Item) bool {
	other := than.(MVCCKey)
	cmp := bytes.Compare(k.Key, other.Key)
	if cmp < 0 {
		return true
	}
	if cmp > 0 {
		return false
	}
	// Более новые версии (больший инвертированный timestamp) идут первыми
	return k.Timestamp > other.Timestamp
}

// bytesCompare сравнивает два байтовых среза.
func bytesCompare(a, b []byte) int {
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		if a[i] < b[i] {
			return -1
		}
		if a[i] > b[i] {
			return 1
		}
	}
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return 0
}