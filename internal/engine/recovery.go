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
	"log"

	"github.com/f4ga/ScoriaDB/internal/mvcc"
)

// recoverFromWAL recovers the database state from WAL.
func recoverFromWAL(wal *WAL, memTable *MemTable, vlog *VLog) error {
	return wal.Recover(func(entry *WalEntry) error {
		switch entry.Op {
		case OpPut:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			if len(entry.Value) == 12 {
				if vp, ok := decodeValuePointer(entry.Value); ok {
					if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
						log.Printf("wal: skipping entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
							vp.Offset, vp.Size, vlog.Size())
						return nil
					}
				}
			}
			memTable.Put(mvccKey, entry.Value)
		case OpDelete:
			mvccKey := mvcc.NewMVCCKey(entry.Key, entry.Timestamp)
			memTable.Put(mvccKey, nil)
		case OpBatch:
			ops, err := decodeBatchOps(entry.Value)
			if err != nil {
				log.Printf("wal: failed to decode batch: %v", err)
				return nil
			}
			for _, op := range ops {
				mvccKey := mvcc.NewMVCCKey(op.Key, entry.Timestamp)
				if op.IsDelete {
					memTable.Put(mvccKey, nil)
					continue
				}
				if len(op.Value) == 12 {
					if vp, ok := decodeValuePointer(op.Value); ok {
						if vp.Offset < 0 || vp.Size <= 0 || vp.Offset+int64(vp.Size)+8 > vlog.Size() {
							log.Printf("wal: skipping batch entry with invalid VLog pointer offset=%d size=%d vlogSize=%d",
								vp.Offset, vp.Size, vlog.Size())
							memTable.Put(mvccKey, nil)
							continue
						}
					} else {
						log.Printf("wal: skipping batch entry with malformed VLog pointer")
						memTable.Put(mvccKey, nil)
						continue
					}
				}
				memTable.Put(mvccKey, op.Value)
			}
		}
		return nil
	})
}
