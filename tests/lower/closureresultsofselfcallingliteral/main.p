package main

func main() {
	var f func(n int) func() int
	f = func(n int) func() int {
		if n < 2 {
			return func() int { return n }
		}
		return f(n - 1)
	}
	println(f(5)())
}
