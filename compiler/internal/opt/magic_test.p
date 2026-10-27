// Ported from internal/opt/magic_test.go at the go-cutover tag.

package opt

import (
	"pluto/bits"
	"pluto/math"
	"pluto/testing"

	"compiler/compat"
)

func mulhiu(x, y uint64) uint64 {
	hi, _ := bits.Mul64(x, y)
	return hi
}

func mulhi(x, y int64) int64 {
	hi, _ := bits.Mul64(uint64(x), uint64(y))
	// Signed high half from the unsigned one.
	r := int64(hi)
	if x < 0 {
		r -= y
	}
	if y < 0 {
		r -= x
	}
	return r
}

// The sequences below mirror unsignedDivConst and signedDivConst exactly.

func udivMagicEval(x, c uint64) uint64 {
	s, m := umagic(c)
	if m&1 == 0 {
		return mulhiu(x, 1<<63+m/2) >> (s - 1)
	}
	hi := mulhiu(x, m)
	return ((x-hi)>>1 + hi) >> (s - 1)
}

func sdivMagicEval(x, c int64) int64 {
	s, m := smagic(c)
	var q int64
	if m&1 == 0 {
		q = mulhi(x, int64(m/2)) >> (s - 1)
	} else {
		q = (mulhi(x, int64(m)) + x) >> s
	}
	return q - (x >> 63)
}

// magicRand is a deterministic splitmix64 generator. It stands in for
// Go's math/rand seeded with 1, which the library does not have: the
// numbers differ, but they only widen the set of operands.
type magicRand struct{ s uint64 }

func (r *magicRand) Uint64() uint64 {
	r.s += 0x9e3779b97f4a7c15
	z := r.s
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

func (r *magicRand) Intn(n int) int { return int(r.Uint64() % uint64(n)) }

func TestMagicDivision(t *testing.T) {
	r := magicRand{1}
	divisors := compat.Append[uint64](nil, 3, 5, 6, 7, 9, 10, 11, 12, 13, 100, 641, 1000, 1<<20*3, 1e9+7, math.MaxUint32, 1<<63-1, 1<<63+1, math.MaxUint64)
	for i := 0; i < 300; i++ {
		divisors = compat.Append(divisors, r.Uint64()>>uint(r.Intn(63)))
	}
	xs := compat.Append[uint64](nil, 0, 1, 2, 3, 1<<32, math.MaxUint32, 1<<63-1, 1<<63, math.MaxUint64, math.MaxUint64-1)
	for i := 0; i < 500; i++ {
		xs = compat.Append(xs, r.Uint64()>>uint(r.Intn(64)))
	}
	for _, c := range divisors {
		if c < 2 || c&(c-1) == 0 {
			continue
		}
		for _, x := range xs {
			if got, want := udivMagicEval(x, c), x/c; got != want {
				t.Fatalf("unsigned %d / %d = %d, want %d", x, c, got, want)
			}
		}
		if c > math.MaxInt64 {
			continue
		}
		sc := int64(c)
		for _, ux := range xs {
			for _, x := range [2]int64{int64(ux), -int64(ux)} {
				if got, want := sdivMagicEval(x, sc), x/sc; got != want {
					t.Fatalf("signed %d / %d = %d, want %d", x, sc, got, want)
				}
			}
		}
	}
}
