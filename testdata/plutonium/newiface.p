package main

// Returning a value whose methods have pointer receivers as an interface
// (language spec sections 8.6 and 13.9): a pointer to a local value would be
// E3001, so the value is built on the heap with new, and the interface is
// declared owner so that it takes the box. This is the fix that the E0021
// message gives for a return statement
// (tests/diag/ptr_recv_return).

import "pluto/fmt"

type Owned owner interface{ Area() int }

type Square struct{ side int }

func (s *Square) Area() int { return s.side * s.side }

func buildOwned() Owned {
	return new(Square{5})
}

func main() {
	o := buildOwned()
	fmt.Println(o.Area())
}
