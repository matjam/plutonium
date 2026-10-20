package main

// A self-calling literal returns its func parameter; the recursive call passes a closure capturing main's local; stored globally.
var keep func() int

func main() {
	x := 7
	var f func(g func() int, n int) func() int
	f = func(g func() int, n int) func() int {
		if n == 0 {
			return g
		}
		return f(func() int { return x }, n-1)
	}
	keep = f(nil, 2)
	println(keep())
}
