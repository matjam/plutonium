package main

// Two literals calling each other; one returns a closure capturing main's local, stored in a package variable.
var keep func() int

func main() {
	x := 5
	var a, b func(n int) func() int
	a = func(n int) func() int {
		if n == 0 {
			return func() int { return x }
		}
		return b(n - 1)
	}
	b = func(n int) func() int {
		return a(n)
	}
	keep = b(3)
	println(keep())
}
