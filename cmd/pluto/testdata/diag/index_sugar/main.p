package main

import "pluto/vec"

// An index expression on a container calls its methods (spec section
// 11.11): v[i] reads with Index, v[i] = x writes with SetIndex, and the
// escape analysis checks those calls as it checks the calls written out.

var all = vec.Make[*int](2)

// Global stores a pointer to a local variable in a package vector.
func Global() {
	x := 1
	all[0] = &x
}

type Log struct{ n int }

var lastSet *int

// SetIndex keeps the pointer it is given.
func (l *Log) SetIndex(i int, p *int) { lastSet = p }
func (l *Log) Index(i int) *int       { return lastSet }

// Local writes through a local container whose SetIndex keeps the value.
func Local() {
	var l Log
	y := 2
	l[0] = &y
}

// Fine: the element is a package variable.
var n int

func Fine() int {
	all[1] = &n
	return *all[1]
}

func main() {
	Global()
	Local()
	println(Fine())
}
