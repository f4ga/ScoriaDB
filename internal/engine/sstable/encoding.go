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

package sstable

import (
	"encoding/binary"
	"errors"
	"scoriadb/internal/mvcc"
)

// encodeMVCCKey кодирует MVCCKey в байты для хранения в SSTable.
// Формат: длина ключа (2 байта) + ключ + timestamp (8 байт, little endian)
func encodeMVCCKey(key mvcc.MVCCKey) []byte {
	kl := len(key.Key)
	buf := make([]byte, 2+kl+8)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(kl))
	copy(buf[2:2+kl], key.Key)
	binary.LittleEndian.PutUint64(buf[2+kl:], key.Timestamp)
	return buf
}

// decodeMVCCKey декодирует байты обратно в MVCCKey.
func decodeMVCCKey(data []byte) (mvcc.MVCCKey, error) {
	if len(data) < 2 {
		return mvcc.MVCCKey{}, ErrCorrupted
	}
	kl := binary.LittleEndian.Uint16(data[0:2])
	if len(data) < int(2+kl+8) {
		return mvcc.MVCCKey{}, ErrCorrupted
	}
	userKey := data[2 : 2+kl]
	timestamp := binary.LittleEndian.Uint64(data[2+kl:])
	return mvcc.MVCCKey{
		Key:       userKey,
		Timestamp: timestamp,
	}, nil
}

// ErrCorrupted ошибка повреждённых данных.
var ErrCorrupted = errors.New("corrupted SSTable data")
