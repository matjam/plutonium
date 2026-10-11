package main

// Every place the language moves or drops an owner (language spec sections
// 18.5 and 18.6), each checked to drop every constructed owner exactly
// once. Conn's Drop prints its id; the zero value, which a variable or
// place holds before it is given a value and after a move or take, prints
// "drop empty". A variable or place the compiler knows holds nothing is not
// dropped at all, so "drop empty" appears only where a value may have been
// moved on some paths only, or where a place was emptied with take. The
// program also runs under the debug allocator, which fails on a leak.

import "pluto/rt"

type Conn owner struct{ id int }

var made, dropped [64]int

func mk(id int) Conn {
	made[id]++
	return Conn{id: id}
}

func (c *Conn) Name() int { return c.id }

func (c *Conn) Drop() {
	if c.id == 0 {
		println("  drop empty")
		return
	}
	dropped[c.id]++
	println("  drop", c.id)
}

// Close consumes its receiver, which it drops when it returns.
func (c Conn) Close() { println("  close", c.id) }

type Pair owner struct {
	x, y Conn
}

type node owner struct {
	conn Conn
	next Conn
}

type Res owner interface {
	Name() int
}

type Slot owner union {
	Empty
	Open Conn
	Code int
}

func open(id int) Conn { return mk(id) }

func consume(c Conn) { println("  consume", c.id) }

// show borrows its argument for the call only.
func show(c *Conn) { println("  show", c.id) }

func keep(c Conn) Conn { return c }

func pass(c Conn) { consume(c) } // c moved on: not dropped here

func lvl3(id int) Conn { return mk(id) }
func lvl2(id int) Conn { c := lvl3(id); return c }
func lvl1(id int) Conn { return lvl2(id) }

func named(id int) (c Conn) {
	c = mk(id)
	return
}

func namedReturn(id int) (c Conn) {
	return mk(id)
}

func namedTwice(id int) (c Conn) {
	c = mk(id)
	return mk(id + 1) // c's value is dropped when the return replaces it
}

func early(id int, stop bool) Conn {
	a := mk(id)
	b := mk(id + 1)
	if stop {
		consume(a)
		return b // a moved, b returned: nothing dropped here
	}
	consume(b)
	return a
}

func deferred(id int) {
	c := mk(id)
	defer c.Drop()
	println("  deferred body", c.id)
}

func deferredClosure(id int) {
	c := mk(id)
	defer func() { println("  deferred sees", c.id) }()
	d := mk(id + 1)
	consume(d)
}

func leaves(id int) {
	a := mk(id)
	b := mk(id + 1)
	consume(a)
	rt.Goexit()
	println("not reached", b.id)
}

func main() {
	println("return")
	{
		a := open(1)
		println("  got", a.id)
	}

	println("fresh variable")
	{
		a := mk(2)
		b := a
		println("  b", b.id)
	}

	println("existing variable")
	{
		var c Conn
		c = mk(3) // c held nothing
		c = mk(4) // drops 3
		println("  c", c.id)
	}

	println("field")
	{
		var p Pair
		p.x = mk(5)
		p.y = mk(6)
		p.x = mk(7) // drops 5
		println("  p", p.x.id, p.y.id)
	}

	println("box field")
	{
		b := mk(8)
		n := new(node)
		n.conn = b
		n.next = mk(9)
		n.conn = mk(10) // drops 8
		println("  held", n.conn.id, n.next.id)
	}

	println("array element")
	{
		var arr [3]Conn
		arr[1] = mk(11)
		arr[1] = mk(12) // drops 11
		arr[2] = mk(13)
		println("  arr", arr[0].id, arr[1].id, arr[2].id)
	}

	println("argument")
	{
		c := mk(14)
		consume(c)
		pass(mk(15))
		d := mk(16)
		pass(d)
	}

	println("several levels")
	{
		c := lvl1(17)
		println("  c", c.id)
		c = keep(c) // c moved out and back
		println("  kept", c.id)
	}

	println("named results")
	{
		a := named(18)
		b := namedReturn(19)
		c := namedTwice(20)
		println("  named", a.id, b.id, c.id)
	}

	println("composite literal")
	{
		a := mk(22)
		p := Pair{x: a, y: mk(23)}
		println("  pair", p.x.id, p.y.id)
		q := Pair{y: mk(24)}
		println("  half", q.y.id)
	}

	println("take")
	{
		n := new(node)
		n.conn = mk(25)
		c := take(&n.conn)
		println("  took", c.id)
	}

	println("union")
	{
		s := Slot.Open(mk(26))
		s = Slot.Code(1) // drops 26
		s = Slot.Open(mk(27))
		switch v := s.(type) {
		case Open:
			println("  open", v.id)
		default:
			println("  not reached")
		}
	}

	println("owning interface")
	{
		var r Res = new(mk(28))
		r = new(mk(29)) // drops 28
		println("  r", r.Name())
		c := new(mk(30))
		var q Res
		q = c
		println("  q", q.Name())
		switch v := q.(type) {
		case owned[Conn]:
			println("  asserted", v.id)
		}
	}

	println("early return")
	{
		a := early(31, true)
		b := early(33, false)
		println("  early", a.id, b.id)
	}

	println("defer")
	deferred(35)
	deferredClosure(36)

	println("goexit")
	println("  exited", rt.CatchGoexit(func() { leaves(38) }))

	println("loop")
	for i := 0; i < 3; i++ {
		c := mk(40 + i)
		if i == 1 {
			consume(c)
			continue
		}
		if i == 2 {
			d := mk(43)
			consume(d)
			break
		}
		println("  iteration", c.id)
	}

	println("consuming method and explicit drop")
	{
		c := mk(44)
		c.Close()
		d := mk(45)
		d.Drop()
	}

	println("temporaries")
	{
		println("  id", mk(46).id)
		_ = mk(47)
	}

	println("new(v)")
	{
		c := mk(48)
		b := new(c)
		println("  boxed", b.id)
	}

	println("moved on some paths")
	{
		c := mk(49)
		if made[49] > 0 {
			consume(c)
		}
	} // c may hold a value: dropped, and holds the zero value here

	println("borrow then move")
	{
		b := mk(50)
		show(&b) // a view for the call only
		n := new(node)
		n.conn = b // b holds nothing from here on, and is not dropped
		c := mk(51)
		println("  name", c.Name())
		consume(c)
		var arr [2]Conn
		arr[0] = mk(52)
		show(&arr[0])
		consume(take(&arr[0])) // arr holds nothing again
	}

	println("check")
	ok := true
	for i := range made {
		if made[i] != dropped[i] {
			println("  id", i, "made", made[i], "dropped", dropped[i])
			ok = false
		}
	}
	println("  ok", ok)
}
