package main

// Integer arithmetic at every width, with Go's wrapping, shift and
// division semantics.

func i8ops(a, b int8) {
	println("i8", a+b, a-b, a*b, a/b, a%b, a&b, a|b, a^b, a&^b, -a, ^a)
}

func u8ops(a, b uint8) {
	println("u8", a+b, a-b, a*b, a/b, a%b, a&b, a|b, a^b, a&^b, -a, ^a)
}

func i16ops(a, b int16) {
	println("i16", a+b, a-b, a*b, a/b, a%b, a<<3, a>>2)
}

func u16ops(a, b uint16) {
	println("u16", a+b, a-b, a*b, a/b, a%b, a<<3, a>>2)
}

func i32ops(a, b int32) {
	println("i32", a+b, a-b, a*b, a/b, a%b, a<<5, a>>3)
}

func u32ops(a, b uint32) {
	println("u32", a+b, a-b, a*b, a/b, a%b, a<<5, a>>3)
}

func i64ops(a, b int64) {
	println("i64", a+b, a-b, a*b, a/b, a%b, a<<7, a>>5)
}

func u64ops(a, b uint64) {
	println("u64", a+b, a-b, a*b, a/b, a%b, a<<7, a>>5)
}

func shifts(x int64, ux uint64, n uint) {
	println("shift", n, x<<n, x>>n, ux<<n, ux>>n)
}

func divs(a, b int64) {
	println("div", a, b, a/b, a%b)
}

func conv(x int64) {
	println("conv", int8(x), uint8(x), int16(x), uint16(x), int32(x), uint32(x), uint64(x))
}

func main() {
	i8ops(100, 27)
	i8ops(-128, -1)
	i8ops(127, 3)
	u8ops(250, 10)
	u8ops(3, 250)
	i16ops(30000, 7)
	i16ops(-32768, 3)
	u16ops(65535, 2)
	i32ops(2147483647, 2)
	i32ops(-7, 2)
	u32ops(4294967295, 16)
	i64ops(9223372036854775807, 3)
	i64ops(-9223372036854775808, -1)
	u64ops(18446744073709551615, 10)
	for _, n := range []uint{0, 1, 31, 32, 63, 64, 65, 200} {
		shifts(-12345, 0xF00DF00DCAFEBEEF, n)
	}
	divs(7, 2)
	divs(-7, 2)
	divs(7, -2)
	divs(-7, -2)
	divs(-9223372036854775808, -1)
	for _, x := range []int64{0, 1, -1, 127, 128, 255, 256, 65535, 65536, -129, 4294967295, 4294967296, -9223372036854775808} {
		conv(x)
	}
	var acc uint32 = 1
	for i := 0; i < 40; i++ {
		acc = acc*2654435761 + uint32(i)
	}
	println("acc", acc)
	var h uint64 = 14695981039346656037
	for i := 0; i < 100; i++ {
		h ^= uint64(i)
		h *= 1099511628211
	}
	println("fnv", h)
}
