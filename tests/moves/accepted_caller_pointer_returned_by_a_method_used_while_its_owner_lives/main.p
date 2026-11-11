package main

// A pointer a method of a local owner returns is a view of the owner
// (section 18.3): it may be used while the owner lives, and the caller's
// memory it points at may be moved after the call.

type C owner struct{ n int }
type O owner struct{ p *C }

func (o *O) P() *C { return o.p }

func drop(o O) {}

func take(c C) {}

func f(c *C) int {
	o := O{p: c}
	p := o.P()
	n := p.n
	drop(o)
	return n
}

func main() {
	c := C{n: 1}
	println(f(&c))
	take(c)
}
