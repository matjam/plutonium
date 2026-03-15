package main

// Demonstrates if/else and for loops.
// Computes sum of 1..10 = 55, then checks if > 50.
//
// Build: pluto build -o flow ./examples/03-control-flow/main.pu
// Run:   ./flow; echo $?
// Expected output: 55

func sumTo(n int64) int64 {
	s := int64(0)
	for i := int64(1); i <= n; i++ {
		s = s + i
	}
	return s
}

func check(n int64) int64 {
	if n > 50 {
		return n
	}
	return 0
}

func main() int64 {
	total := sumTo(10)
	return check(total)
}
