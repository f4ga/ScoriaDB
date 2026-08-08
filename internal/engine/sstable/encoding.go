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

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// encodeMVCCKey encodes MVCCKey into bytes for storage in SSTable.
// Format: key length (4 bytes, uint32) + key (variable) + timestamp (8 bytes, little endian)
// The 4-byte length matches the format used by parseIndex in reader.go.
// See: PROMPT-SSTABLE-FINAL
func encodeMVCCKey(key mvcc.MVCCKey) []byte {
	kl := len(key.Key)
	buf := make([]byte, 4+kl+8)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(kl))
	copy(buf[4:4+kl], key.Key)
	binary.LittleEndian.PutUint64(buf[4+kl:], key.Timestamp)
	return buf
}

// decodeMVCCKey decodes bytes back to MVCCKey.
func decodeMVCCKey(data []byte) (mvcc.MVCCKey, error) {
	if len(data) < 4 {
		return mvcc.MVCCKey{}, ErrCorrupted
	}
	kl := binary.LittleEndian.Uint32(data[0:4])
	if len(data) < int(4+kl+8) {
		return mvcc.MVCCKey{}, ErrCorrupted
	}
	userKey := data[4 : 4+kl]
	timestamp := binary.LittleEndian.Uint64(data[4+kl:])
	return mvcc.MVCCKey{
		Key:       userKey,
		Timestamp: timestamp,
	}, nil
}

// ErrCorrupted is returned when SSTable data is corrupted.
var ErrCorrupted = errors.New("corrupted SSTable data")

// Type tags for stored values, mirroring the engine-level tags. The sstable
// package cannot import the engine package (import cycle), so the byte values
// are duplicated here and must stay in sync with engine.TypeInline /
// TypeValuePointer / TypeTombstone. See DEF-02 / DEF-04.
const (
	tagInline    = 0x00
	tagValuePtr  = 0x01
	tagTombstone = 0x02
)

// isValidValueTag reports whether b is a well-known value tag. A value whose
// first byte is not a valid tag is treated as the legacy (untagged) format.
func isValidValueTag(b byte) bool {
	return b == tagInline || b == tagValuePtr || b == tagTombstone
}
