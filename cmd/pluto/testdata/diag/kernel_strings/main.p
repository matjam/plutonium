package main

import "pluto/fmt"

// Building strings allocates, which kernel code may do only in functions
// marked //pu:alloc (language spec section 12.7).

func label(name string) string {
	return "irq " + name
}

func copied(b []byte) string {
	return string(b)
}

func formatted(n int) string {
	return fmt.Sprintf("irq %d", n)
}

//pu:alloc
func joined(a, b string) string {
	return a + "/" + b // allowed: the function says it allocates
}

func viewed(s string) int {
	// Literals, comparison, slicing, indexing and len never allocate.
	if s == "irq" && len(s[1:]) > 1 && s[0] == 'i' {
		return 1
	}
	return 0
}

func main() {
	var b [2]byte
	_ = label("x")
	_ = copied(b[:])
	_ = formatted(1)
	_ = joined("a", "b")
	_ = viewed("irq")
}
