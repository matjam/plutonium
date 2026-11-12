package main

// A member value cannot use the enum being declared (spec section 7.12,
// E2310), through a member, a conversion or len.

type C enum {
	A
	B = C.A
}

type D enum {
	A
	B = D(1)
}

type E enum {
	A
	B = len(E)
}

func main() {}
