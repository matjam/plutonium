package main

// Division and remainder by constants, which the optimiser turns into
// shifts and multiplications.

func div64(x int64) int64 {
	return x/2 + x/3 + x/7 + x/10 + x/16 + x/-4 + x/-9 + x/1000000007 + x%2 + x%3 + x%8 + x%-5 + x%641
}

func divu64(x uint64) uint64 {
	return x/2 + x/3 + x/7 + x/10 + x/16 + x/1000000007 + x%2 + x%3 + x%8 + x%641 + x/0x8000000000000001
}

func div32(x int32) int32 {
	return x/3 + x/5 + x/8 + x/-7 + x%3 + x%16 + x%-6
}

func divu32(x uint32) uint32 {
	return x/3 + x/5 + x/8 + x/7 + x%3 + x%16 + x/4294967295
}

func div16(x int16) int16  { return x/3 + x/-5 + x%7 + x%4 }
func divu16(x uint16) uint16 { return x/3 + x/10 + x%7 + x%4 }
func div8(x int8) int8     { return x/3 + x/-2 + x%5 }
func divu8(x uint8) uint8   { return x/3 + x/10 + x%7 }

func even(x int64) bool { return x%2 == 0 }
func odd(x int64) bool  { return x%2 != 0 }

func main() {
	xs := []int64{0, 1, -1, 2, -2, 3, 7, -7, 100, -100, 1000000007, -1000000008,
		9223372036854775807, -9223372036854775808, 12345678901234, -98765432109876}
	var s int64
	var u uint64
	var s32 int32
	var u32 uint32
	var s16 int16
	var u16 uint16
	var s8 int8
	var u8 uint8
	evens := 0
	for _, x := range xs {
		s += div64(x)
		u += divu64(uint64(x))
		s32 += div32(int32(x))
		u32 += divu32(uint32(x))
		s16 += div16(int16(x))
		u16 += divu16(uint16(x))
		s8 += div8(int8(x))
		u8 += divu8(uint8(x))
		if even(x) {
			evens++
		}
		if odd(x) {
			evens += 100
		}
		println(x, div64(x), divu64(uint64(x)), div32(int32(x)), divu32(uint32(x)))
	}
	println("sums", s, u, s32, u32, s16, u16, s8, u8, evens)
}
