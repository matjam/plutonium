package main

import "pluto/fmt"

// fmt.Println walks w and calls the String method of the T it holds in
// place. String asserts its interface field to *U and keeps the address
// of a field of what it points to, which is u, a frame variable: E3003
// (language spec section 18.3).

type U struct{ n int }

type T struct{ i any }

var kept *int

func (t *T) String() string {
	if u, ok := t.i.(*U); ok {
		kept = &u.n
	}
	return ""
}

type W struct{ t T }

func f() {
	var u U
	w := W{t: T{i: &u}}
	fmt.Println(&w)
}

func main() {
	f()
	println(kept != nil)
}
