package main

// One argument to a function whose other parameters have defaults is an
// ordinary argument: an owner passed by value moves into the parameter
// (language spec sections 11.6 and 18.1).

type Buf owner struct{ n int }

func take(b Buf, scale int = 2) int { return b.n * scale }

func main() {
	b := Buf{n: 3}
	println(take(b))
	println(b.n)
}
