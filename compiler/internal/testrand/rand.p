// Package testrand is the part of Go's math/rand/v2 that the compiler's unit
// tests use: the PCG source (PCG-DXSM, as in Go) and the Rand methods drawn
// from it. Given the same seeds it yields the same numbers as Go's
// rand.New(rand.NewPCG(seed1, seed2)), so a ported property test runs over
// the same cases as the Go test did.
package testrand

import "pluto/bits"

// Rand is a PCG generator with the methods of Go's rand.Rand.
type Rand struct {
	hi uint64
	lo uint64
}

// New returns a generator seeded as Go's rand.New(rand.NewPCG(seed1, seed2)).
func New(seed1, seed2 uint64) Rand { return Rand{hi: seed1, lo: seed2} }

func (p *Rand) next() (uint64, uint64) {
	const (
		mulHi = 2549297995355413924
		mulLo = 4865540595714422341
		incHi = 6364136223846793005
		incLo = 1442695040888963407
	)
	// state = state * mul + inc
	hi, lo := bits.Mul64(p.lo, mulLo)
	hi += p.hi*mulLo + p.lo*mulHi
	lo, c := bits.Add64(lo, incLo, 0)
	hi, _ = bits.Add64(hi, incHi, c)
	p.lo = lo
	p.hi = hi
	return hi, lo
}

// Uint64 returns a uniformly distributed 64-bit value.
func (p *Rand) Uint64() uint64 {
	hi, lo := p.next()
	// DXSM, "double xorshift multiply", as Go's PCG.
	const cheapMul = 0xda942042e4dd58b5
	hi ^= hi >> 32
	hi *= cheapMul
	hi ^= hi >> (3 * 16)
	hi *= lo | 1
	return hi
}

func (p *Rand) uint64n(n uint64) uint64 {
	if n&(n-1) == 0 { // n is a power of two, can mask
		return p.Uint64() & (n - 1)
	}
	hi, lo := bits.Mul64(p.Uint64(), n)
	if lo < n {
		thresh := -n % n
		for lo < thresh {
			hi, lo = bits.Mul64(p.Uint64(), n)
		}
	}
	return hi
}

// Uint64N returns a value in [0, n). It panics if n is 0.
func (p *Rand) Uint64N(n uint64) uint64 {
	if n == 0 {
		panic("invalid argument to Uint64N")
	}
	return p.uint64n(n)
}

// IntN returns a value in [0, n). It panics if n <= 0.
func (p *Rand) IntN(n int) int {
	if n <= 0 {
		panic("invalid argument to IntN")
	}
	return int(p.uint64n(uint64(n)))
}
