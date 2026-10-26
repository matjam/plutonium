package main

import "pluto/fmt"

// A pointer to a type parameter converted to an interface has the methods of
// a pointer to each type the parameter is instantiated with. The escape
// analysis tells the types converted apart by identity: the type parameters
// named T of first and show are different types (spec section 18.3).

type S struct{ n int }

var keepS *S

func (s *S) String() string {
	keepS = s
	return "s"
}

func first[T any](v *T) { fmt.Println(v) }

func show[T any](v *T) { fmt.Println(v) }

func main() {
	n := 1
	first(&n)
	s := S{42}
	show(&s)
}
