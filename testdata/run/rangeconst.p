package main

// Ranging over a string constant: untyped, typed, and of a named string
// type.

const letters = "aßc"

const typed string = "xy"

type name string

const named name = "héllo"

func main() {
	for i, c := range letters {
		println(i, c)
	}
	for i := range typed {
		println("typed", i)
	}
	n := 0
	for _, c := range named {
		n += int(c)
	}
	println("named", n)
	for range "abc" {
		n++
	}
	println("literal", n)
}
