package hash

import "pluto/bits"

// SipHash24 is an allocation-free SipHash-2-4 stream with a 128-bit key
// and a 64-bit result. Initialize it with NewSipHash24 or SetSeed; other
// operations panic on an uninitialized value, including empty writes.
// Assignment copies the stream independently, including its key.
//
// A secret unpredictable key is required for hash-flooding defense. The
// caller supplies the key; this type obtains no entropy. An explicitly
// supplied all-zero key is allowed for deterministic hashing. SipHash is
// not a general-purpose unkeyed cryptographic digest.
//
// Algorithm by Jean-Philippe Aumasson and Daniel J. Bernstein. Reference
// and test vectors: https://github.com/veorq/SipHash (CC0).
type SipHash24 struct {
	key         [16]byte
	v0, v1      uint64
	v2, v3      uint64
	tail        uint64
	length      uint64
	used        uint8
	initialized bool
}

// NewSipHash24 returns an empty stream using a copy of key.
func NewSipHash24(key [16]byte) SipHash24 {
	var h SipHash24
	h.SetSeed(key)
	return h
}

// SetSeed replaces the key with a copy of key and discards prior writes.
func (h *SipHash24) SetSeed(key [16]byte) {
	h.key = key
	h.initialized = true
	h.Reset()
}

// Seed returns a copy of the key. It panics if h has not been initialized.
func (h SipHash24) Seed() [16]byte {
	h.requireKey()
	return h.key
}

func (h *SipHash24) requireKey() {
	if !h.initialized {
		panic("hash: SipHash24 requires a key")
	}
}

// Reset discards prior writes while preserving the key. It panics if h
// has not been initialized.
func (h *SipHash24) Reset() {
	h.requireKey()
	var k0, k1 uint64
	for i := 0; i < 8; i++ {
		k0 |= uint64(h.key[i]) << (8 * i)
		k1 |= uint64(h.key[i+8]) << (8 * i)
	}
	h.v0 = 0x736f6d6570736575 ^ k0
	h.v1 = 0x646f72616e646f6d ^ k1
	h.v2 = 0x6c7967656e657261 ^ k0
	h.v3 = 0x7465646279746573 ^ k1
	h.tail, h.length, h.used = 0, 0, 0
}

func (h *SipHash24) round() {
	h.v0 += h.v1
	h.v1 = bits.RotateLeft64(h.v1, 13) ^ h.v0
	h.v0 = bits.RotateLeft64(h.v0, 32)
	h.v2 += h.v3
	h.v3 = bits.RotateLeft64(h.v3, 16) ^ h.v2
	h.v0 += h.v3
	h.v3 = bits.RotateLeft64(h.v3, 21) ^ h.v0
	h.v2 += h.v1
	h.v1 = bits.RotateLeft64(h.v1, 17) ^ h.v2
	h.v2 = bits.RotateLeft64(h.v2, 32)
}

func (h *SipHash24) compress(word uint64) {
	h.v3 ^= word
	h.round()
	h.round()
	h.v0 ^= word
}

func (h *SipHash24) put(c byte) {
	h.tail |= uint64(c) << (8 * h.used)
	h.used++
	h.length++
	if h.used == 8 {
		h.compress(h.tail)
		h.tail, h.used = 0, 0
	}
}

// Write consumes all bytes of b and returns len(b), nil. It retains no view.
func (h *SipHash24) Write(b []byte) (int, error) {
	h.requireKey()
	for _, c := range b {
		h.put(c)
	}
	return len(b), nil
}

// WriteString consumes all bytes of s and returns len(s), nil.
func (h *SipHash24) WriteString(s string) (int, error) {
	h.requireKey()
	for i := 0; i < len(s); i++ {
		h.put(s[i])
	}
	return len(s), nil
}

// WriteByte writes c and returns nil.
func (h *SipHash24) WriteByte(c byte) error {
	h.requireKey()
	h.put(c)
	return nil
}

// Sum64 returns the hash without changing the stream. Writes may continue
// after any number of sums. It panics if h has not been initialized.
func (h SipHash24) Sum64() uint64 {
	h.requireKey()
	// Only the low length byte is encoded in the final word, as SipHash defines.
	h.compress(h.tail | h.length<<56)
	h.v2 ^= 0xff
	for i := 0; i < 4; i++ {
		h.round()
	}
	return h.v0 ^ h.v1 ^ h.v2 ^ h.v3
}
