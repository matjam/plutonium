package main

// A self-calling literal returns a closure that captures a local of main; the result is stored in a package variable.
var keep func() int

func main() {
	x := 5
	var f func(n int) func() int
	f = func(n int) func() int {
		if n < 2 {
			return func() int { return x + n }
		}
		return f(n - 1)
	}
	keep = f(5)
	println(keep())
}
