package main

import "pluto/strings"

type Shape interface {
	Area() int
}

type Square struct{ side int }

func perimeter(side int) int { return 4 * side }

func Zero[T any]() T {
	var z T
	return z
}

func sign(x int) int {
	if x < 0 {
		return -1
	}
}

func main() {
	var count int
	name := "three"
	count = name
	var total int64 = 2
	sum := count + total
	println(cuont, sum)
	println(perimeter(1, 2))
	var s Shape = Square{2}
	z := Zero()
	var spare int
	_, _ = s, z
}
