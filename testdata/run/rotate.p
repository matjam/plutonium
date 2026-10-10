package main

// Rotations written with shifts, at every width and in the Or, Xor and Add
// forms, plus shift pairs that are not rotations.

//pu:noinline
func rotr32(x uint32, n uint32) uint32 { return (x >> n) | (x << (32 - n)) }

//pu:noinline
func r7(x uint32) uint32 { return rotr32(x, 7) }

//pu:noinline
func l13x(x uint64) uint64 { return x<<13 ^ x>>51 }

//pu:noinline
func l3a(x uint16) uint16 { return x<<3 + x>>13 }

//pu:noinline
func l1(x uint8) uint8 { return x>>7 | x<<1 }

// Not rotations: the counts do not add up to the width, or the operands
// differ.
//
//pu:noinline
func notRot(x, y uint32) (uint32, uint32) { return x<<3 | x>>28, x<<4 | y>>28 }

func main() {
	println(r7(0x80000001), rotr32(0x12345678, 8), l13x(0xF00000000000000F), l3a(0xE001), l1(0x81))
	a, b := notRot(0xF000000F, 0x10000000)
	println(a, b)
}
