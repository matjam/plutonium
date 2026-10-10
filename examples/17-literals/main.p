package main

import "pluto/fmt"

// Demonstrates hex literals, char literals, and bitwise AND-NOT.
//
// Build: pluto build -o literals ./examples/17-literals/
// Run:   ./literals
// Expected output:
//   hex=255
//   char=65
//   newline=10
//   andnot=240

// NOTE: the &^ operation is done in a helper function to work around
// a pre-existing register allocation bug that manifests when &^
// (which lowers to XOR + AND) is used inline with many live variables.

func clearLowNibble(x int64) int64 {
	return x &^ int64(0x0F)
}

func main() int64 {
	// Hex literals
	h := 0xFF
	fmt.Printf("hex=%d\n", h)

	// Char literals
	ch := 'A'
	fmt.Printf("char=%d\n", ch)

	// Newline char
	nl := '\n'
	fmt.Printf("newline=%d\n", nl)

	// Bitwise AND-NOT (clear low nibble)
	y := clearLowNibble(0xFF)
	fmt.Printf("andnot=%d\n", y)

	return 0
}
