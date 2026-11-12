package main

// A union type is written only as the type of a type definition (spec
// section 7.13, E2401): not as the type of a variable, field or parameter.

var x union { A int }

var y []union { B string }

type S struct {
	u union { C int }
}

func f(p union { D int }) {}

func main() {
	var z union { E int }
	_ = z
}
