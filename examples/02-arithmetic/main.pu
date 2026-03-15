package main

// Basic integer arithmetic with function calls.
// The exit code is the result of the computation.
//
// Build: pluto build -o arith ./examples/02-arithmetic/main.pu
// Run:   ./arith; echo $?
// Expected output: 42

func add(a, b int64) int64 {
	return a + b
}

func sub(a, b int64) int64 {
	return a - b
}

func mul(a, b int64) int64 {
	return a * b
}

func main() int64 {
	x := add(20, 1)  // x = 21
	y := mul(x, 2)   // y = 42
	return y
}
