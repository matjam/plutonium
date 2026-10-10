package main

import "unsafe"

// print and println of floats: the shortest form that reads back to the
// same value, in strconv's 'g' format, for float64 and float32.

func f64(bits uint64) float64 { return *(*float64)(unsafe.Pointer(&bits)) }
func f32(bits uint32) float32 { return *(*float32)(unsafe.Pointer(&bits)) }

type celsius float64

func main() {
	zero := 0.0
	negZero := -zero
	inf := 1 / zero
	nan := zero / zero
	println("special", zero, negZero, inf, -inf, nan, -nan)
	var z32 float32
	println("special32", z32, -z32, float32(inf), float32(-inf), float32(nan))

	println("simple", 1.0, -1.0, 1.5, -2.25, 0.1, 0.2, 0.1+0.2, 1.0/3, 2.0/3, 100.0, 123456.0, 1234567.0)
	println("thresholds", 1e5, 999999.0, 1e6, 1e-4, 0.00012345, 1e-5, 0.000099)
	println("large", 1e18, 1e19, 1e20, 1e21, 1e22, 1e100, 1e300, 1.7976931348623157e308)
	println("small", 1e-10, 1e-100, 1e-300, 2.2250738585072014e-308)
	println("subnormal", 2.225073858507201e-308, 1e-310, 5e-324, 4.9406564584124654e-324, -5e-324)
	println("exact", 9007199254740992.0, 9007199254740993.0, 4503599627370496.5, 123456789012345678.0)
	println("float32", float32(0.1), float32(1)/3, float32(16777216), float32(3.4028234663852886e38), float32(1e-45), float32(1.17549435e-38), float32(-2.5))
	println("named", celsius(36.6), celsius(-40))
	print("print ", 2.5, " ", float32(0.3), " ", -1e-7, "\n")

	// Powers of two and ten across the whole range.
	p := 1.0
	for i := 0; i < 1100; i += 37 {
		println("pow2", i, p, -1/p)
		for j := 0; j < 37; j++ {
			p *= 2
		}
	}
	q := float32(1)
	for i := 0; i < 50; i++ {
		println("pow10", i, q, 1/q)
		q *= 10
	}

	// Pseudo-random bit patterns: every exponent, any mantissa.
	x := uint64(0x9e3779b97f4a7c15)
	for i := 0; i < 300; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		println("r64", f64(x), f64(x>>12), f32(uint32(x)), f32(uint32(x>>40)))
	}
}
