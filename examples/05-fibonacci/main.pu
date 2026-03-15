package main

// Computes Fibonacci numbers iteratively.
// No recursion needed — just a loop.
//
// fib(10) = 55
//
// Build: pluto build -o fib ./examples/05-fibonacci/main.pu
// Run:   ./fib; echo $?
// Expected output: 55

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
	return fib(10)
}
