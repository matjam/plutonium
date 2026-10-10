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
	xs := []int{1, 2, 3, 4}
	println(level1(xs, 0))
	println(level1(xs, 2))
}
