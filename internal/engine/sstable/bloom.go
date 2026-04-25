package sstable

import (
	"hash"
	"hash/fnv"
)

// BloomFilter реализует фильтр Блума для быстрой проверки отсутствия ключа.
type BloomFilter struct {
	bits []byte
	k    uint32 // количество хеш-функций
}

// NewBloomFilter создает новый фильтр Блума с заданным количеством бит на ключ.
func NewBloomFilter(bitsPerKey int) *BloomFilter {
	// Вычисляем размер битового массива на основе ожидаемого количества ключей
	// Для простоты используем фиксированный размер, но можно динамически вычислять
	// Используем формулу: m = n * bitsPerKey, где n - ожидаемое количество ключей
	// Поскольку мы не знаем n заранее, используем начальный размер 1024 байта (8192 бита)
	// и масштабируем при необходимости.
	// В данной реализации мы будем использовать фиксированный размер для MVP.
	size := 1024 // байты
	return &BloomFilter{
		bits: make([]byte, size),
		k:    uint32(bitsPerKey), // упрощенно, на самом деле нужно вычислять оптимальное k
	}
}

// Add добавляет ключ в фильтр Блума.
func (bf *BloomFilter) Add(key []byte) {
	// Используем двойное хеширование (алгоритм из LevelDB)
	h1, h2 := bloomHash(key)
	for i := uint32(0); i < bf.k; i++ {
		pos := (h1 + i*h2) % uint32(len(bf.bits)*8)
		bf.setBit(pos)
	}
}

// MayContain проверяет, может ли ключ содержаться в фильтре Блума.
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

// setBit устанавливает бит в позиции pos (0-indexed) в 1.
func (bf *BloomFilter) setBit(pos uint32) {
	byteIndex := pos / 8
	bitIndex := pos % 8
	if byteIndex >= uint32(len(bf.bits)) {
		// Расширяем bits при необходимости
		newSize := byteIndex + 1
		newBits := make([]byte, newSize)
		copy(newBits, bf.bits)
		bf.bits = newBits
	}
	bf.bits[byteIndex] |= 1 << bitIndex
}

// getBit возвращает true, если бит в позиции pos установлен.
func (bf *BloomFilter) getBit(pos uint32) bool {
	byteIndex := pos / 8
	bitIndex := pos % 8
	if byteIndex >= uint32(len(bf.bits)) {
		return false
	}
	return (bf.bits[byteIndex]>>bitIndex)&1 == 1
}

// Encode возвращает сериализованный фильтр Блума (байты).
func (bf *BloomFilter) Encode() []byte {
	// Для простоты возвращаем битовый массив как есть
	return bf.bits
}

// DecodeBloomFilter создает BloomFilter из сериализованных данных.
func DecodeBloomFilter(data []byte, k uint32) *BloomFilter {
	return &BloomFilter{
		bits: data,
		k:    k,
	}
}

// SetK устанавливает количество хеш-функций (для тестов).
func (bf *BloomFilter) SetK(k uint32) {
	bf.k = k
}

// bloomHash возвращает два 32-битных хеша для ключа (алгоритм LevelDB).
func bloomHash(key []byte) (uint32, uint32) {
	// Используем FNV-1a хеш для простоты (в LevelDB используется хеш от MurmurHash2)
	var h hash.Hash32 = fnv.New32a()
	h.Write(key)
	h1 := h.Sum32()

	// Второй хеш - просто инвертированный первый (для MVP)
	h2 := h1 ^ 0xbc9f1d34
	return h1, h2
}

// optimalBitsPerKey вычисляет оптимальное количество бит на ключ для заданной вероятности ошибки.
//nolint:unused // will be used for dynamic Bloom sizing
func optimalBitsPerKey(falsePositiveRate float64) int {
	// Формула: m = -n * ln(p) / (ln(2)^2)
	// Для p=0.01 получаем примерно 9.6 бит на ключ
	// Округляем до 10
	return 10
}