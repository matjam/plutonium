package main

import (
	"pluto/bits"
	"pluto/fmt"
)

// Bit manipulation with pluto/bits.

func main() {
	var flags uint64
	bits.Set(&flags, 0)
	bits.Set(&flags, 3)
	bits.Set(&flags, 7)
	fmt.Printf("flags=%d\n", flags) // 1 + 8 + 128

	bits.Clear(&flags, 3)
	fmt.Printf("flags=%d\n", flags)
	fmt.Printf("bit7=%t bit3=%t\n", bits.Test(flags, 7), bits.Test(flags, 3))

	bits.Toggle(&flags, 0)
	fmt.Printf("flags=%d\n", flags)

	var word uint32 = 0xABCD1234
	fmt.Printf("extract=%#x\n", bits.Extract(word, 8, 8))
	bits.Insert(&word, 0, 8, 0xFF)
	fmt.Printf("insert=%#x\n", word)

	fmt.Printf("ones=%d tz=%d lz=%d len=%d\n",
		bits.OnesCount64(0xF0F0), bits.TrailingZeros64(0x80), bits.LeadingZeros64(1), bits.Len64(255))
	fmt.Printf("rotl=%#x swap=%#x\n", bits.RotateLeft32(0x80000001, 1), bits.ReverseBytes32(0x11223344))
	hi, lo := bits.Mul64(0xFFFFFFFFFFFFFFFF, 2)
	fmt.Printf("mul64 hi=%d lo=%#x\n", hi, lo)
}
