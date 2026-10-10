package main

// work and iwork recurse twice, so neither inlining nor tail recursion
// removes the calls that the loops below keep values live across.
func work(x float64) float64 {
	if x < 2 {
		return x
	}
	return work(x-1)*0.5 + work(x-2)*0.25
}

func iwork(x int) int {
	if x < 2 {
		return x
	}
	return iwork(x-1) + iwork(x-2)
}

func floats(n int) float64 {
	a, b, c, d, e, f := 1.0, 2.0, 3.0, 4.0, 5.0, 6.0
	for i := 0; i < n; i++ {
		a, b, c, d, e, f = b+work(a), c, d+a, e+work(b), f*1.5, a-b
	}
	return a + b + c + d + e + f
}

func ints(n int) int {
	a, b, c, d, e, f, g, h := 1, 2, 3, 4, 5, 6, 7, 8
	for i := 0; i < n; i++ {
		a, b, c, d, e, f, g, h = b+iwork(a&7), c, d+a, e+iwork(b&7), f*3, g-b, h^a, a+h
	}
	return a + b + c + d + e + f + g + h
}

func main() {
	println(int(floats(10)*1e6), ints(10))
}
