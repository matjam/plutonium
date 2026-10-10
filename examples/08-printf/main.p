package main

import "pluto/fmt"

// fmt.Printf with Go's verbs, flags, widths and precisions.

func main() {
	fmt.Printf("%d\n", 42)
	fmt.Printf("hello %d\n", 17)
	fmt.Printf("x=%x X=%X %#x\n", 42, 42, 42)
	fmt.Printf("%c\n", 65)
	fmt.Printf("%d%%\n", 100)
	fmt.Printf("[%6d] [%-6d] [%06d] [%+d]\n", 42, 42, 42, 42)
	fmt.Printf("%.2f %e %g %v\n", 3.14159, 1234.5678, 0.000123, 2.5)
	fmt.Printf("%q %v %t %T\n", "quoted", "plain", true, 3.0)
	fmt.Printf("%b %o %U\n", 5, 64, 0x1F600)
}
