package main

// A Drop method may keep a copy of what its receiver holds, and pointers
// it loads from the owner that the owner does not own.

type O owner struct {
	n int
	g *int
}

var last int

var seen *int

var shared int

func (o *O) Drop() {
	last = o.n
	seen = o.g
}

func main() {
	var o O
	o.n = 3
	o.g = &shared
	println(o.n)
}
