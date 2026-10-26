package main

// len of a ring calls its Len method, which has a pointer receiver, so the
// operand must be addressable or a pointer; cap does not call methods
// (spec section 13.6).

func mk() ring[int, 2] {
	var r ring[int, 2]
	return r
}

func main() {
	var r ring[int, 4]
	println(len(r))
	println(len(mk()))
	println(cap(r))
}
