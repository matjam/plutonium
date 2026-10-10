package main

import "pluto/fmt"

// Numeric, character and string literals, and bitwise operators.

func main() {
	fmt.Printf("hex=%d octal=%d binary=%d underscores=%d\n", 0xFF, 0o17, 0b1010, 1_000_000)
	fmt.Printf("char=%d newline=%d unicode=%d\n", 'A', '\n', '世')
	fmt.Printf("andnot=%d xor=%d shift=%d\n", 0xFF&^0x0F, 0b1100^0b1010, 1<<10)
	fmt.Printf("raw=%s escaped=%q\n", `C:\path`, "tab\there")
	const big = 1 << 62
	fmt.Printf("big=%d float=%g\n", big, 6.02e23)
}
