package main

// An untyped constant takes the element type of volatile[T] and
// aligned[T, N] (spec sections 7.15 and 7.16), so it must be representable
// in T, and it is still not a member of an enum T (section 8.4).

type Color enum {
	Red
	Green
}

var a volatile[uint8] = 300

var b aligned[int8, 16] = 1.5

var c volatile[Color] = 1

func f(x aligned[uint16, 8]) {}

func g() volatile[bool] { return 1 }

func main() {
	f(-1)
}
