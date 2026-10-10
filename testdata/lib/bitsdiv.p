package main

// bits.Div64: 128-by-64-bit division, checked on fixed cases (results
// from Go's math/bits) and on pseudo-random ones by multiplying back.

import "pluto/bits"

func main() {
	cases := [...]struct{ hi, lo, y uint64 }{
		{0, 100, 7},
		{1, 0, 3},
		{0x0123456789abcdef, 0xfedcba9876543210, 0xffffffffffffffff},
		{12345, 67890, 1 << 63},
		{0x7fffffffffffffff, 0xffffffffffffffff, 0x8000000000000000},
		{5, 0, 6},
	}
	for _, c := range cases {
		q, r := bits.Div64(c.hi, c.lo, c.y)
		println(q, r)
	}
	// x = 6364136223846793005*x + 1442695040888963407 (Knuth's MMIX LCG).
	x := uint64(42)
	next := func() uint64 {
		x = 6364136223846793005*x + 1442695040888963407
		return x
	}
	bad := 0
	for i := 0; i < 100000; i++ {
		y := next() >> (next() % 64)
		if y == 0 {
			y = 1
		}
		hi := next() % y
		lo := next()
		q, r := bits.Div64(hi, lo, y)
		ph, pl := bits.Mul64(q, y)
		pl, c := bits.Add64(pl, r, 0)
		ph += c
		if r >= y || ph != hi || pl != lo {
			bad++
		}
	}
	println("random failures", bad)
}
