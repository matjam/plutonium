package main

// A pointer to caller memory that a method of a local owner returns is a
// view of the owner (section 18.3), so f may not return it once the owner
// is moved; and it keeps its tie to the caller: the caller's c is moved
// while q, which points at it, is still used.

type C owner struct{ n int }
type O owner struct{ p *C }

func (o *O) P() *C { return o.p }

func drop(o O) {}

func take(c C) {}

func f(c *C) *C {
	o := O{p: c}
	p := o.P()
	drop(o)
	return p
}

func main() {
	c := C{n: 1}
	q := f(&c)
	take(c)
	println(q.n)
}
