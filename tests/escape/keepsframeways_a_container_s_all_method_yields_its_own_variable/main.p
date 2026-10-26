package main

import "pluto/iter"
type C struct{ n int }
func (c *C) All() iter.Seq2[int, *int] {
	return func(yield func(int, *int) bool) {
		local := c.n
		yield(0, &local)
	}
}
var keep *int
func f() {
	c := C{1}
	for _, p := range c {
		keep = p
	}
}
func main() { f() }
