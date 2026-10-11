package main

import "unsafe"

// An exhaustive switch on an enum value with no default case is a
// terminating statement, and panics for a value that is not a member
// (spec sections 12.1 and 12.9).

type Color enum {
	Red
	Green
	Blue
}

func name(c Color) string {
	switch c {
	case Red:
		return "red"
	case Green:
		return "green"
	case Blue:
		return "blue"
	}
}

func main() {
	println(name(Color.Green))
	var raw uint8 = 7
	c := *(*Color)(unsafe.Pointer(&raw))
	println(name(c))
}
