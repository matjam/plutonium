// Integer formatting through fmt: every combination of the verbs b, o, O,
// d, x and X with the flags #, 0, -, + and space, widths and precisions,
// on positive, zero, negative and extreme values of several sizes, then
// %x and %X on strings and byte slices with the # and space flags.
// Go counts the 0x, 0b and 0o prefixes outside the width of a zero-padded
// number (%#010x of 0x642d is 0x000000642d), and a precision turns zero
// padding off; the output is compared with Go's.

package main

import (
	"pluto/fmt"
)

func main() {
	verbs := []string{"b", "o", "O", "d", "x", "X"}
	flagSets := []string{"", "#", "0", "-", "+", " ", "#0", "#-", "#+", "# ", "0+", "0 ", "-0", "+ ", "#0+", "#-+", "#0 ", "-+ "}
	widths := []string{"", "1", "5", "10"}
	precs := []string{"", ".0", ".1", ".6", ".12"}
	ints := []int{0, 1, 5, 8, 255, 0x642d, -1, -5, -0x642d}
	for _, v := range verbs {
		for _, fl := range flagSets {
			for _, w := range widths {
				for _, p := range precs {
					f := "[%" + fl + w + p + v + "]"
					fmt.Printf(f+" ", ints[0])
					for _, x := range ints[1:] {
						fmt.Printf(f, x)
					}
					fmt.Println()
				}
			}
		}
	}

	// Extremes and sizes.
	for _, f := range []string{"%#x", "%#o", "%#b", "%#O", "%#030x", "%+#030b", "%#-30o|", "% #22X", "%#.20x", "%#025.20x"} {
		fmt.Printf(f+"\n", int64(-1<<63))
		fmt.Printf(f+"\n", uint64(1<<64-1))
		fmt.Printf(f+"\n", int8(-128))
		fmt.Printf(f+"\n", uint8(200))
		fmt.Printf(f+"\n", int16(-300))
		fmt.Printf(f+"\n", uint32(0xdeadbeef))
	}

	// The cases of the bug report.
	fmt.Printf("%#010x|%#10x|%#-10x|%010x|%#010X|%#010o|%#010b|%#010O\n",
		0x642d, 0x642d, 0x642d, 0x642d, 0x642d, 0x642d, 5, 0x642d)
	fmt.Printf("%#010x|%#+010x|%# 010x|%#.4x|%#010.4x\n", -0x642d, 0x642d, 0x642d, 0x642d, 0x642d)

	// Strings and byte slices.
	strs := []string{"", "a", "hello", "\x00zé"}
	for _, f := range []string{"%x", "%X", "%#x", "%#X", "% x", "% X", "%# x", "%# X", "%10x", "%-10x|", "%010x", "%#10x", "%#010x", "%#-10x|", "%.3x", "%#.3x", "%# .3x", "%8.3x", "%#8.3x", "%-#8.3x|", "%08.3x"} {
		for _, s := range strs {
			// Bytes of s, with 0xff (not valid UTF-8) after a long one.
			var buf [8]byte
			n := copy(buf[:], s)
			if n > 3 {
				buf[n] = 0xff
				n++
			}
			fmt.Printf("["+f+"]", s)
			fmt.Printf("["+f+"]", buf[:n])
		}
		fmt.Println()
	}
	var arr [3]byte
	arr[0], arr[1], arr[2] = 1, 0xab, 255
	for _, f := range []string{"%x", "%#x", "% x", "%# x", "%08x", "%#08x"} {
		fmt.Printf("["+f+"]", arr)
		fmt.Printf("["+f+"]", arr[:])
		fmt.Println()
	}
}
