package main

func level3(xs []int, i int) int {
	return xs[i] + 1
}

func level2(xs []int, i int) int {
	return level3(xs, i+1) * 2
}

func level1(xs []int, i int) int {
	return level2(xs, i+1) - 1
}

func main() {
	xs := data[:]
	println(level1(xs, 0))
	println(level1(xs, 2))
}

// The values live in a global so that the optimiser cannot work out the
// first call at compile time: it has to run, inlined, at line 17.
var data = [4]int{1, 2, 3, 4}
