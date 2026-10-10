package main

// Shifts of shifts by constants, where the counts add up to the width or
// more, or wrap around when added as 64-bit numbers. Found by
// tools/difftest: (x/2) >> (2^64-1) was folded to x >> 0.

var g32 int32 = -17
var g64 int64 = -12345
var gu uint32 = 0xdeadbeef

func main() {
	var zero uint64
	big := 18446744073709551615 - zero // 2^64-1, known only after folding
	println((g32/2)>>big, (g32>>1)>>big, (g32>>3)>>30, (g32>>20)>>20)
	println((g64>>40)>>40, (g64>>1)>>(big-1), (g64/4)>>big)
	println((gu>>3)>>29, (gu>>16)>>16, (gu<<16)<<16, (gu<<1)<<big, (gu>>1)>>big)
	println((gu>>3)>>28, (gu<<3)<<28, (g32<<3)<<28)
	var s uint = 63
	println((g64>>1)>>s, (g64>>s)>>s)
}
