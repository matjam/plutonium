package main

import "pluto/fmt"

// Demonstrates %s string formatting.
//
// Build: pluto build -o strings ./examples/18-strings/
// Run:   ./strings
// Expected output:
//   hello world
//   answer=42

func main() int64 {
	fmt.Printf("hello %s\n", "world")
	fmt.Printf("%s=%d\n", "answer", 42)
	return 0
}
