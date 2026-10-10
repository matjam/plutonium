package main

import "pluto/fmt"

// First Plutonium program with stdout output.
// Prints Fibonacci numbers to demonstrate working I/O.
//
// Build: pluto build -o hello ./examples/07-hello/main.pu
// Run:   ./hello
// Expected output:
//   1
//   1
//   2
//   3
//   5
//   8
//   13
//   21
//   34
//   55

func fib(n int64) int64 {
	if n <= 1 {
		return n
	}
	a := int64(0)
	b := int64(1)
	for i := int64(2); i <= n; i++ {
		next := a + b
		a = b
		b = next
	}
	return b
}

func main() int64 {
	for i := int64(1); i <= 10; i++ {
		fmt.PrintInt(fib(i))
		fmt.Println()
	}
	return 0
}
