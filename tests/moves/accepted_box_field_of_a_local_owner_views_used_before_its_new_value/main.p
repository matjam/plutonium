package main

// Views into the object of a box field, used before the field is given a
// new value or made again after it, and a view into another owner's box
// (language spec section 18.3).

type L owner struct{ x int }
type N owner struct {
	c owned[L]
	d owned[L]
}

func main() {
	n := N{c: new(L), d: new(L)}
	n.c.x = 7
	p := &n.c.x
	println(*p)
	n.c = new(L)
	p = &n.c.x
	*p = 8
	println(n.c.x)
	q := &n.d.x
	n.c = new(L)
	println(*q)
	m := N{c: new(L), d: new(L)}
	r := &m.c.x
	n.c = new(L)
	println(*r)
}
