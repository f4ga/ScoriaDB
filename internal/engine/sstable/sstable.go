package sstable

import (
	"scoriadb/internal/mvcc"
)

// SSTable представляет заглушку SSTable (будет реализована на этапе 2).
type SSTable struct{}

// Lookup ищет ключ в SSTable и возвращает значение, если найдено.
func (s *SSTable) Lookup(key mvcc.MVCCKey) ([]byte, bool) {
	// Заглушка: всегда не найдено
	return nil, false
}

// NewSSTable создает новую заглушку SSTable.
func NewSSTable() *SSTable {
	return &SSTable{}
}