package main

// A packed type may be written only as the type of a type definition
// (spec section 10.8, E2104): not as the type of a package variable, a
// parameter, a local variable, a struct field or an alias.

type Ctl packed uint8 {
	a bits[uint8, 8]
}

var x packed uint8 {
	a bits[uint8, 8]
}

func f(p packed uint8 { a bits[uint8, 8] }) {}

type S struct {
	c packed uint8 { a bits[uint8, 8] }
}

type A = packed uint8 { a bits[uint8, 8] }

func main() {
	var y packed uint8 { a bits[uint8, 8] }
	var c Ctl
	_, _ = y, c
}
