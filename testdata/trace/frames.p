package main

// The trace passes through a frame that saves every callee-saved register,
// RBP among them (pressure), and one that keeps RBP as a frame pointer
// because it realigns its stack (wide).

//pu:noinline
func pick(xs []int, i int) int {
	return xs[i]
}

//pu:noinline
func pressure(xs []int, n int) int {
	a, b, c, d, e, f, g := n+1, n*2, n*3, n^5, n-7, n*11, n+13
	r := pick(xs, n)
	return r + a + b + c + d + e + f + g
}

//pu:noinline
func wide(n int) int {
	var buf aligned[[8]int, 64]
	for i := 0; i < 8; i++ {
		buf[i] = n + i
	}
	return pressure(buf[:], n) * 2
}

func main() {
	println(wide(3))
	println(wide(9))
}
