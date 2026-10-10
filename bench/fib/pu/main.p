// fib: naive doubly-recursive Fibonacci. Measures call/return overhead.
package main

import "pluto/fmt"

const N = 43

func fib(n int64) int64 {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func main() int64 {
	fmt.Printf("%d\n", fib(N))
	return 0
}
