package main

// Short-circuit conditions made branch-free become Or and And of bools.

//pu:noinline
func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' }

//pu:noinline
func inRange(x, lo, hi int) bool { return x >= lo && x < hi }

//pu:noinline
func either(a, b int) bool { return a > 0 || b > 0 }

func main() {
	n := 0
	s := "a b\tc\nd\re  f"
	for i := 0; i < len(s); i++ {
		if isSpace(s[i]) {
			n++
		}
	}
	println(n, inRange(3, 1, 5), inRange(5, 1, 5), inRange(0, 1, 5), either(0, 1), either(-1, 0))
}
