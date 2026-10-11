package main

// Views of local owners used after the owner moved, was dropped or was
// given a new value (language spec section 18.3), the forms
// testdata/plutonium/dropsites.p must not contain.

type Conn owner struct{ id int }

func (c *Conn) Drop()         {}
func (c *Conn) Name() int     { return c.id }
func (c *Conn) Self() *Conn   { return c }
func (c *Conn) Clone() Conn   { return Conn{id: c.id} }
func consume(c Conn)          {}
func link(h *Holder, c *Conn) { h.p = c }
func verbose() bool           { return true }

type Holder struct {
	p *Conn
	n int
}

func main() {
	// A pointer, written through after the move.
	c := Conn{id: 1}
	p := &c
	consume(c)
	*p = Conn{id: 2}

	// A slice of a local array of owners, after the array is replaced.
	var arr [2]Conn
	s := arr[:]
	arr = [2]Conn{}
	s[1].id = 3

	// A method value, after an explicit drop.
	d := Conn{id: 4}
	m := d.Name
	d.Drop()
	println(m())

	// A view in a field of a local struct; other fields stay usable.
	e := Conn{id: 5}
	h := Holder{p: &e}
	consume(e)
	println(h.n)
	println(h.p.id)

	// A view a call stores into another argument, moved on some paths.
	f := Conn{id: 6}
	var h2 Holder
	link(&h2, &f)
	if verbose() {
		consume(f)
	}
	println(h2.p.id)

	// A view a method returns, after its owner is given a new value.
	g := Conn{id: 7}
	q := g.Self()
	g = Conn{id: 8}
	println(q.id)

	// A view of an owner whose block has ended.
	var r *Conn
	{
		k := Conn{id: 9}
		r = &k
	}
	println(r.id)

	// A function literal that uses a view.
	l := Conn{id: 10}
	pl := &l
	show := func() { println(pl.id) }
	consume(l)
	show()

	// A view stored through a pointer, where the compiler cannot follow it.
	var held *Conn
	pp := &held
	o := Conn{id: 11}
	*pp = &o
	consume(o)
	println(held.id)
}
