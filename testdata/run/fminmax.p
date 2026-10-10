package main

// min and max of floats: min(-0, +0) is -0 and max(-0, +0) is +0 in
// either order, a NaN operand gives NaN, and infinities order as usual.
// Found by tools/difftest.

var gz int32

func neg(x float64) bool { return 1/x < 0 } // true for -0 and negative values

func neg32(x float32) bool { return 1/x < 0 }

func main() {
	z := float64(gz)
	nz := -z
	nan := z / z
	inf := 1 / z
	println(neg(min(z, nz)), neg(min(nz, z)), neg(max(z, nz)), neg(max(nz, z)))
	println(neg(min(z, z)), neg(min(nz, nz)), neg(max(z, z)), neg(max(nz, nz)))
	println(neg(min(z, nz, z)), neg(max(nz, z, nz)), neg(min(1, z, nz)), neg(max(-1, nz, z)))
	m := min(nan, 1)
	n := max(2, nan)
	println(m != m, n != n, min(inf, 3) == 3, max(-inf, 3) == 3, max(inf, 3) == inf)
	println(int64(min(2.5, -1.25)*4), int64(max(2.5, -1.25)*4), min(z, 1) == 0)

	z32 := float32(gz)
	nz32 := -z32
	println(neg32(min(z32, nz32)), neg32(min(nz32, z32)), neg32(max(z32, nz32)), neg32(max(nz32, z32)))
	nan32 := z32 / z32
	m32 := max(nan32, z32)
	println(m32 != m32, min(float32(1.5), 2) == 1.5)
}
