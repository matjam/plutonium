package main

import "pluto/fmt"

// Demonstrates the bits[V] zero-cost bit-addressing lens.
// bits[flags] creates a lens over the flags variable, providing
// Set/Clear/Test/Toggle methods that manipulate individual bits.
//
// Build: pluto build -o bits ./examples/13-bits/
// Run:   ./bits
// Expected output:
//   flags=137
//   flags=129
//   bit7=1
//   bit3=0
//   flags=128

func main() int64 {
	var flags uint64
	var b bits[flags]

	b.Set(0)
	b.Set(3)
	b.Set(7)

	fmt.Printf("flags=%d\n", flags)   // 1 + 8 + 128 = 137

	b.Clear(3)
	fmt.Printf("flags=%d\n", flags)   // 1 + 128 = 129

	t := b.Test(7)
	fmt.Printf("bit7=%d\n", t)        // 1

	t = b.Test(3)
	fmt.Printf("bit3=%d\n", t)        // 0

	b.Toggle(0)
	fmt.Printf("flags=%d\n", flags)   // 128

	return 0
}
