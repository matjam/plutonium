package main

// Chains of comparisons with small constants, which become bit tests, and
// some that must not.

//pu:noinline
func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' }

//pu:noinline
func notSpace(c byte) bool { return c != ' ' && c != '\n' && c != '\t' && c != '\r' }

//pu:noinline
func small(x int8) bool { return x == 0 || x == 5 || x == 63 || x == 5 }

//pu:noinline
func wide(x int64) bool { return x == 1 || x == 2 || x == 40 || x == 62 }

//pu:noinline
func high(x uint16) bool { return x == 3 || x == 64 || x == 7 }

//pu:noinline
func far(x uint16) bool { return x == 3 || x == 67 || x == 7 } // 64 apart

//pu:noinline
func vowel(c byte) bool { return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u' }

//pu:noinline
func top(x int8) bool { return x == -1 || x == -6 || x == -56 }

//pu:noinline
func two(x int) bool { return x == 3 || x == 4 } // too short

//pu:noinline
func mixed(x, y int) bool { return x == 1 || y == 2 || x == 3 }

//pu:noinline
func count(s string) (int, int) {
	n, m := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u' {
			n++
		}
		if c == '!' || c == '?' || c == '.' {
			m++
		}
	}
	return n, m
}

func main() {
	for _, c := range []byte{0, 9, 10, 11, 13, 32, 33, 73, 74, 77, 96, 128, 200, 255} {
		println(c, isSpace(c), notSpace(c))
	}
	for _, x := range []int8{-128, -1, 0, 1, 5, 63, 64, 127} {
		println(x, small(x))
	}
	for _, x := range []int64{-9223372036854775808, -64, -1, 0, 1, 2, 40, 62, 63, 64, 65, 104, 1 << 40} {
		println(x, wide(x))
	}
	for _, x := range []uint16{0, 2, 3, 7, 64, 66, 67, 71, 65535} {
		println(x, high(x), far(x))
	}
	for _, c := range []byte{0, 'a', 'b', 'e', 'i', 'o', 'u', 'v', 'A', 'a' + 64, 255} {
		println(c, vowel(c))
	}
	for _, x := range []int8{-128, -57, -56, -55, -6, -1, 0, 1, 127} {
		println(x, top(x))
	}
	println(two(3), two(4), two(5), mixed(1, 0), mixed(0, 2), mixed(3, 3), mixed(0, 0))
	println(count("Hello, world! Is this a test? Yes. aeiou?!."))
}
