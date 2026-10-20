package main

// Self-calling literal whose closure (capturing the loop variable) is kept past the iteration.


func main() {
	var keep func() int
	var f func(n int, i int) func() int
	f = func(n int, i int) func() int {
		if n < 2 {
			return func() int { return i }
		}
		return f(n-1, i)
	}
	for i := 0; i < 3; i++ {
		keep = f(3, i)
	}
	println(keep())
}
