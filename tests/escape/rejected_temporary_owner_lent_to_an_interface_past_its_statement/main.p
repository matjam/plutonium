package main

// An owner larger than one word lent to an ordinary interface is not
// copied: the data word points at the temporary (section 7.9), so the
// interface is a view of a temporary dropped when the statement ends
// (section 18.3, E3009).

import "pluto/fmt"

type Big owner struct{ a, b, c int }

func makeBig() Big { return Big{1, 2, 3} }

var sink any

func declared() {
	var i any = makeBig()
	fmt.Println(i)
}

func assigned() {
	var i any
	i = makeBig()
	fmt.Println(i)
}

func returned() any {
	return makeBig()
}

func stored() {
	sink = makeBig()
}

func main() {
	declared()
	assigned()
	fmt.Println(returned())
	stored()
}
