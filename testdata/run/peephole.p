package main

// Edge cases for rewrites that depend on facts and on x86 addressing
// arithmetic: even and negative dividends, branch elimination over narrow
// types, and LEA forms wrapping at 32 and 64 bits.

//pu:noinline
func halveEven(n int64) int64 {
	if n%2 == 0 {
		return n / 2
	}
	return n
}

//pu:noinline
func quarter(n int64) int64 {
	if n&3 == 0 {
		return n / 4
	}
	return n / 4 * 10
}

//pu:noinline
func nonneg(n int64) int64 {
	if n >= 0 {
		return n / 8
	}
	return -1
}

//pu:noinline
func step(n int64) int64 {
	if n%2 == 0 {
		return n / 2
	}
	return 3*n + 1
}

//pu:noinline
func step32(n uint32) uint32 {
	if n&1 == 0 {
		return n >> 1
	}
	return 3*n + 1
}

//pu:noinline
func stepI32(n int32) int32 {
	if n%2 == 0 {
		return n / 2
	}
	return 5*n - 7
}

//pu:noinline
func pick8(c bool, a, b int8) int8 {
	r := b
	if c {
		r = a + 1
	}
	return r
}

//pu:noinline
func pickBool(x, y int) bool {
	ok := false
	if x < y {
		ok = x > 0
	}
	return ok
}

//pu:noinline
func lea(x, y int64) (int64, int64, int64, int64) {
	return x*9 + 100, x*8 + y, y + x*4, x*5 - 3
}

//pu:noinline
func lea32(x, y uint32) (uint32, uint32, uint32) {
	return x*9 + 100, x*8 + y, x*3 + 0x7fffffff
}

//pu:noinline
func count(xs []int64) int64 {
	n := int64(0)
	for i := 0; i < len(xs); i++ {
		if xs[i] > 0 {
			n++
		}
	}
	return n
}

func main() {
	for _, n := range []int64{-9, -8, -7, -6, -2, -1, 0, 1, 2, 7, 8, 1 << 62, -1 << 63, 1<<63 - 1} {
		println(n, halveEven(n), quarter(n), nonneg(n), step(n))
	}
	for _, n := range []uint32{0, 1, 2, 3, 1 << 31, 1<<32 - 1, 0x55555555, 0xaaaaaaab} {
		println(n, step32(n))
	}
	for _, n := range []int32{-5, -4, -1, 0, 3, 1 << 30, -1 << 31, 1<<31 - 1} {
		println(n, stepI32(n))
	}
	println(pick8(true, 127, 5), pick8(false, 127, 5), pick8(true, -128, 0))
	println(pickBool(1, 2), pickBool(-1, 2), pickBool(3, 2))
	println(lea(3, 4))
	println(lea(1<<61, -1))
	println(lea(-1<<63, 1<<63-1))
	println(lea32(3, 4))
	println(lea32(1<<31, 1<<32-1))
	println(lea32(0x1c71c71d, 7))
	println(count([]int64{1, -2, 3, 0, 5, -6, 7}))
}
