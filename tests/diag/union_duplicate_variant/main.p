package main

// The variants of a union have distinct names (spec section 7.13, E2404).

type U union {
	A int
	A string
}

type P packed union uint8 {
	t bits[uint8, 1]
	X case 0 {}
	X case 1 {}
}

func main() {}
