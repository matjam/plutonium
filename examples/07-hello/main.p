package main

import "pluto/fmt"

// Prints the first ten Fibonacci numbers.

func fib(n int) int {
	if n <= 1 {
		return n
	}
	a, b := 0, 1
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

func main() {
	for i := 1; i <= 10; i++ {
		fmt.Println(fib(i))
	}
}
