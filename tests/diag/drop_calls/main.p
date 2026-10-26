package main

// The drop of an owner at the end of its scope, and of a temporary owner at
// the end of its statement, calls its Drop method with a pointer to it
// (spec section 18.6). That call is checked as an explicit o.Drop() is.

type Guard owner struct{ hits *int }

var last *int

// Drop keeps what the guard points to.
func (g *Guard) Drop() { last = g.hits }

func newGuard(p *int) Guard { return Guard{hits: p} }

// Scope ends with g, whose Drop keeps &n.
func Scope() {
	n := 0
	g := Guard{hits: &n}
	*g.hits = 1
}

// Temporary drops the guard newGuard returns at the end of the statement.
func Temporary() int {
	n := 0
	return *newGuard(&n).hits
}

var counter int

// Fine: the guard points to a package variable.
func Fine() {
	g := Guard{hits: &counter}
	*g.hits = 2
}

func main() {
	Scope()
	println(Temporary())
	Fine()
}
