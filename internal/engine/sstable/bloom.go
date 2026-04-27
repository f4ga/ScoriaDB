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
	"hash"
	"hash/fnv"
)

// BloomFilter implements a Bloom filter for fast key absence checks.
type BloomFilter struct {
	bits []byte
	k    uint32 // number of hash functions
}

// NewBloomFilter creates a new Bloom filter with given bits per key.
func NewBloomFilter(bitsPerKey int) *BloomFilter {
	// Compute bit array size based on expected number of keys.
	// For simplicity we use a fixed size, but could compute dynamically.
	// Formula: m = n * bitsPerKey, where n is expected key count.
	// Since we don't know n in advance, we use initial size 1024 bytes (8192 bits)
	// and scale if needed.
	// This MVP implementation uses a fixed size.
	size := 1024 // bytes
	return &BloomFilter{
		bits: make([]byte, size),
		k:    uint32(bitsPerKey), // simplified; optimal k should be computed
	}
}

// Add adds a key to the Bloom filter.
func (bf *BloomFilter) Add(key []byte) {
	// Use double hashing (algorithm from LevelDB)
	h1, h2 := bloomHash(key)
	for i := uint32(0); i < bf.k; i++ {
		pos := (h1 + i*h2) % uint32(len(bf.bits)*8)
		bf.setBit(pos)
	}
}

// MayContain checks whether the key may be present in the Bloom filter.
func (bf *BloomFilter) MayContain(key []byte) bool {
	h1, h2 := bloomHash(key)
	for i := uint32(0); i < bf.k; i++ {
		pos := (h1 + i*h2) % uint32(len(bf.bits)*8)
		if !bf.getBit(pos) {
			return false
		}
	}
	return true
}

// setBit sets the bit at position pos (0-indexed) to 1.
func (bf *BloomFilter) setBit(pos uint32) {
	byteIndex := pos / 8
	bitIndex := pos % 8
	if byteIndex >= uint32(len(bf.bits)) {
		// Expand bits if necessary
		newSize := byteIndex + 1
		newBits := make([]byte, newSize)
		copy(newBits, bf.bits)
		bf.bits = newBits
	}
	bf.bits[byteIndex] |= 1 << bitIndex
}

// getBit returns true if the bit at position pos is set.
func (bf *BloomFilter) getBit(pos uint32) bool {
	byteIndex := pos / 8
	bitIndex := pos % 8
	if byteIndex >= uint32(len(bf.bits)) {
		return false
	}
	return (bf.bits[byteIndex]>>bitIndex)&1 == 1
}

// Encode returns serialized Bloom filter bytes.
func (bf *BloomFilter) Encode() []byte {
	// For simplicity, return the bit array as is
	return bf.bits
}

// DecodeBloomFilter creates a BloomFilter from serialized data.
func DecodeBloomFilter(data []byte, k uint32) *BloomFilter {
	return &BloomFilter{
		bits: data,
		k:    k,
	}
}

// SetK sets the number of hash functions (for testing).
func (bf *BloomFilter) SetK(k uint32) {
	bf.k = k
}

// bloomHash returns two 32-bit hashes for a key (LevelDB algorithm).
func bloomHash(key []byte) (uint32, uint32) {
	// Use FNV-1a hash for simplicity (LevelDB uses MurmurHash2)
	var h hash.Hash32 = fnv.New32a()
	h.Write(key)
	h1 := h.Sum32()

	// Second hash is just the first inverted (for MVP)
	h2 := h1 ^ 0xbc9f1d34
	return h1, h2
}
