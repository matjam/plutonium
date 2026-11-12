package main

// min and max take no slice argument: min(s...) is E0150 (spec section
// 13.8).

func main() {
	s := []int{1, 2}
	println(min(s...))
	println(max(s...))
}
