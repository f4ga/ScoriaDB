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
	"math"
	_ "unsafe" // required for go:linkname
)

//go:linkname fastrand runtime.fastrand
func fastrand() uint32

const (
	// DefaultFalsePositiveRate is the target false positive rate (1%).
	// See: ARCH-BLOOM-01
	DefaultFalsePositiveRate = 0.01

	// MinBits is the minimum number of bits for the Bloom filter.
	// Even for a single key we need at least 64 bits to keep k >= 1.
	// See: ARCH-BLOOM-02
	MinBits = 64

	// MaxBits is the maximum number of bits (1.25 MB).
	// Beyond this, the filter becomes too large for the benefit.
	// See: ARCH-BLOOM-03
	MaxBits = 10_000_000

	// MinKeys is the minimum expected keys.
	MinKeys = 1

	// MaxKeys is the maximum expected keys for sizing.
	MaxKeys = 1_000_000

	// bloomFormatVersion is the serialization format version.
	bloomFormatVersion = 1
)

// BloomFilter implements an adaptive Bloom filter for fast key absence checks.
//
// The filter size (m) and number of hash functions (k) are computed optimally
// based on the expected number of keys and target false positive rate.
//
// Formulas:
//
//	m = -n * ln(p) / (ln(2))^2   (number of bits)
//	k = (m / n) * ln(2)          (number of hash functions)
//
// See: ARCH-BLOOM-04, PERF-BLOOM-01
type BloomFilter struct {
	bits []uint64 // bitset, packed as uint64 for efficient access
	m    uint64   // number of bits
	k    uint64   // number of hash functions
	n    uint64   // number of added keys
	seed uint64   // deterministic seed for hashing
}

// NewBloomFilter creates a Bloom filter sized for expectedKeys keys
// with the default false positive rate (1%).
//
// If expectedKeys <= 0, MinKeys is used.
// The filter size is clamped to [MinBits, MaxBits].
//
// See: ARCH-BLOOM-05
func NewBloomFilter(expectedKeys int) *BloomFilter {
	if expectedKeys <= 0 {
		expectedKeys = MinKeys
	}
	if expectedKeys > MaxKeys {
		expectedKeys = MaxKeys
	}

	n := uint64(expectedKeys)
	m := optimalBits(n, DefaultFalsePositiveRate)
	k := optimalHashCount(m, n)

	return &BloomFilter{
		bits: make([]uint64, (m+63)/64),
		m:    m,
		k:    k,
		n:    0,
		seed: uint64(fastrand())<<32 | uint64(fastrand()),
	}
}

// optimalBits computes the optimal number of bits: m = -n * ln(p) / (ln(2))^2
// Result is clamped to [MinBits, MaxBits] and rounded up to nearest uint64 boundary.
//
// See: ARCH-BLOOM-06
func optimalBits(n uint64, p float64) uint64 {
	if n == 0 {
		return MinBits
	}
	ln2 := math.Ln2
	lnP := math.Log(p)
	m := float64(n) * -lnP / (ln2 * ln2)
	mBits := uint64(math.Ceil(m))

	// Round up to nearest multiple of 64 for efficient uint64 packing
	mBits = (mBits + 63) & ^uint64(63)

	if mBits < MinBits {
		mBits = MinBits
	}
	if mBits > MaxBits {
		mBits = MaxBits
	}
	return mBits
}

// optimalHashCount computes the optimal number of hash functions: k = (m/n) * ln(2)
// Minimum 1, maximum 30.
//
// See: ARCH-BLOOM-07
func optimalHashCount(m, n uint64) uint64 {
	if n == 0 {
		return 1
	}
	k := uint64(math.Ceil(float64(m) / float64(n) * math.Ln2))
	if k < 1 {
		return 1
	}
	if k > 30 {
		return 30
	}
	return k
}

// Add adds a key to the Bloom filter.
// Uses double hashing (Kirsch-Mitzenmacher optimization):
//
//	h_i = h1 + i*h2
//
// Only two 64-bit hash computations for any number of hash functions.
//
// See: PERF-BLOOM-02
func (b *BloomFilter) Add(key []byte) {
	if b.m == 0 || b.k == 0 {
		return
	}
	h1, h2 := b.hash(key)
	m := b.m
	bits := b.bits
	for i := uint64(0); i < b.k; i++ {
		pos := (h1 + i*h2) % m
		bits[pos/64] |= uint64(1) << (pos % 64)
	}
	b.n++
}

// MayContain checks whether the key may be present in the Bloom filter.
//
// Returns false if the key is definitely not present.
// Returns true if the key may be present (false positives possible).
//
// Zero allocations in the hot path.
//
// See: PERF-BLOOM-03
//
//go:nosplit
func (b *BloomFilter) MayContain(key []byte) bool {
	if b.m == 0 || b.k == 0 {
		return false
	}
	h1, h2 := b.hash(key)
	m := b.m
	bits := b.bits
	for i := uint64(0); i < b.k; i++ {
		pos := (h1 + i*h2) % m
		if bits[pos/64]&(uint64(1)<<(pos%64)) == 0 {
			return false
		}
	}
	return true
}

// hash computes two 64-bit hash values for the key using the filter's seed.
//
// Uses a fast, non-cryptographic hash based on FNV-1a with 64-bit mixing.
// The two hashes are derived from the same computation (Kirsch-Mitzenmacher).
//
// See: PERF-BLOOM-04
func (b *BloomFilter) hash(key []byte) (uint64, uint64) {
	// FNV-1a 64-bit with seed
	h := b.seed
	for _, c := range key {
		h ^= uint64(c)
		h *= 0x100000001b3 // FNV-1a 64-bit prime
	}

	// Second hash derived from first (double hashing)
	h1 := h
	h2 := h1 ^ 0x9e3779b97f4a7c15 // golden ratio constant

	// Mix second hash
	h2 ^= h2 >> 33
	h2 *= 0xff51afd7ed558ccd
	h2 ^= h2 >> 33
	h2 *= 0xc4ceb9fe1a85ec53
	h2 ^= h2 >> 33

	return h1, h2
}

// Encode serializes the Bloom filter for storage in an SSTable.
//
// Format:
//
//	[Version:1][m:8][k:8][n:8][seed:8][bits:bytes]
//
// The bits array is stored as little-endian uint64 slice.
// Total overhead: 33 bytes.
//
// See: ARCH-BLOOM-08
func (b *BloomFilter) Encode() []byte {
	bitsBytes := len(b.bits) * 8
	buf := make([]byte, 1+8+8+8+8+bitsBytes)
	off := 0

	buf[off] = byte(bloomFormatVersion)
	off++

	binary.LittleEndian.PutUint64(buf[off:], b.m)
	off += 8

	binary.LittleEndian.PutUint64(buf[off:], b.k)
	off += 8

	binary.LittleEndian.PutUint64(buf[off:], b.n)
	off += 8

	binary.LittleEndian.PutUint64(buf[off:], b.seed)
	off += 8

	// Write bits as raw bytes (little-endian uint64 slice)
	for i, v := range b.bits {
		binary.LittleEndian.PutUint64(buf[off+i*8:], v)
	}

	return buf
}

// DecodeBloomFilter deserializes a Bloom filter from SSTable storage.
//
// Supports both the new format (with version header) and the legacy format
// (fixed-size filter without header) for backward compatibility.
//
// If data is empty or nil, returns an empty filter (MayContain always false).
//
// See: ARCH-BLOOM-09
func DecodeBloomFilter(data []byte) *BloomFilter {
	if len(data) == 0 {
		return &BloomFilter{
			bits: nil,
			m:    0,
			k:    0,
			n:    0,
			seed: 0,
		}
	}

	// Check for legacy format: old filters had seed (4 bytes) + bits (1024 bytes)
	// New format starts with version byte == 1
	if len(data) >= 1 && data[0] == byte(bloomFormatVersion) {
		return decodeNewFormat(data)
	}

	// Legacy format: seed (4 bytes) + bits (variable, typically 1024 bytes)
	// We reconstruct with n=0 (unknown) and compute k from the bit array size.
	return decodeLegacyFormat(data)
}

// decodeNewFormat reads the new serialization format.
func decodeNewFormat(data []byte) *BloomFilter {
	if len(data) < 1+8+8+8+8 {
		return &BloomFilter{
			bits: nil,
			m:    0,
			k:    0,
			n:    0,
			seed: 0,
		}
	}

	off := 1 // skip version
	m := binary.LittleEndian.Uint64(data[off:])
	off += 8
	k := binary.LittleEndian.Uint64(data[off:])
	off += 8
	n := binary.LittleEndian.Uint64(data[off:])
	off += 8
	seed := binary.LittleEndian.Uint64(data[off:])
	off += 8

	bitsLen := int((m + 63) / 64)
	bitsBytes := bitsLen * 8
	if off+bitsBytes > len(data) {
		// Truncated data, return empty filter
		return &BloomFilter{
			bits: nil,
			m:    0,
			k:    0,
			n:    0,
			seed: 0,
		}
	}

	bits := make([]uint64, bitsLen)
	for i := 0; i < bitsLen; i++ {
		bits[i] = binary.LittleEndian.Uint64(data[off+i*8:])
	}

	return &BloomFilter{
		bits: bits,
		m:    m,
		k:    k,
		n:    n,
		seed: seed,
	}
}

// decodeLegacyFormat reads the legacy serialization format.
// Legacy format: seed (4 bytes) + bits (variable bytes).
// We compute m and k from the bit array size.
func decodeLegacyFormat(data []byte) *BloomFilter {
	if len(data) < 4 {
		return &BloomFilter{
			bits: nil,
			m:    0,
			k:    0,
			n:    0,
			seed: 0,
		}
	}

	// Skip seed (4 bytes), rest is bit array
	bitsBytes := len(data) - 4
	m := uint64(bitsBytes * 8)

	// Compute k from m and a reasonable n estimate
	// Legacy filters used fixed size, so we estimate n from m
	// For legacy: m = 1024*8 = 8192 bits, bitsPerKey = 10, so n ≈ 819
	n := uint64(float64(m) * 0.1) // rough estimate: 10 bits per key
	k := optimalHashCount(m, n)

	// Convert byte slice to uint64 slice
	numUint64 := (bitsBytes + 7) / 8
	bits := make([]uint64, numUint64)
	for i := 0; i < bitsBytes; i++ {
		byteIdx := i / 8
		bitIdx := uint(i%8) * 8
		bits[byteIdx] |= uint64(data[4+i]) << bitIdx
	}

	return &BloomFilter{
		bits: bits,
		m:    m,
		k:    k,
		n:    0,
		seed: uint64(fastrand())<<32 | uint64(fastrand()),
	}
}

// Len returns the number of keys added to the filter.
func (b *BloomFilter) Len() int {
	return int(b.n)
}

// FalsePositiveRate returns the current estimated false positive rate.
//
// Formula: p = (1 - e^(-k*n/m))^k
//
// See: ARCH-BLOOM-10
func (b *BloomFilter) FalsePositiveRate() float64 {
	if b.m == 0 || b.k == 0 {
		return 1.0
	}
	exp := -float64(b.k) * float64(b.n) / float64(b.m)
	p := math.Pow(1-math.Exp(exp), float64(b.k))
	return p
}

// BitCount returns the number of bits set to 1.
func (b *BloomFilter) BitCount() uint64 {
	var count uint64
	for _, v := range b.bits {
		count += uint64(popcount(v))
	}
	return count
}

// popcount counts the number of set bits in a 64-bit word.
func popcount(x uint64) int {
	// Hacker's Delight, Figure 5-2
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	x = x + (x >> 8)
	x = x + (x >> 16)
	x = x + (x >> 32)
	return int(x & 0x7f)
}
