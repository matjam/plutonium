package main

// Drops of owners along every kind of control flow (language spec section
// 18.6): each constructed owner is dropped exactly once, including where a
// variable is moved inside a loop, a switch or select clause, the right
// operand of && or ||, a range-over-func loop body, after a goto, where
// something other than the variable itself (a closure, a deferred method
// call) gives it a value after a move, and where a pointer writes it
// before one (a view may not be used after the move, section 18.3). Conn's
// Drop prints its id, and "drop empty" for the zero value, which a
// variable holds where it may have been moved on some paths only, or where
// its address was kept. The program also runs under the debug allocator,
// which fails on a leak.

type Conn owner struct{ id int }

var made, dropped [64]int

func mk(id int) Conn {
	made[id]++
	return Conn{id: id}
}

func (c *Conn) Drop() {
	if c.id == 0 {
		println("  drop empty")
		return
	}
	dropped[c.id]++
	println("  drop", c.id)
}

func (c *Conn) Reset(id int) { *c = mk(id) }

func consume(c Conn) { println("  consume", c.id) }

func eat(c Conn) bool {
	consume(c)
	return true
}

func seq(n int) func(func(int) bool) {
	return func(yield func(int) bool) {
		for i := 0; i < n; i++ {
			if !yield(i) {
				return
			}
		}
	}
}

func loops() {
	for i := 0; i < 2; i++ {
		c := mk(1 + i)
		for j := 0; j < 2; j++ {
			d := mk(3 + 2*i + j)
			if j == 0 {
				consume(d)
				continue
			}
			if i == 1 {
				consume(c)
				break
			}
		}
	}
}

func switches(k int) {
	c := mk(10 + k)
	switch k {
	case 0:
		consume(c)
	case 1:
		d := mk(20)
		consume(d)
		fallthrough
	case 2:
		println("  kept", c.id)
	}
}

func shortCircuit(b bool) {
	c := mk(13)
	if b && eat(c) {
		println("  ate")
	}
	d := mk(14)
	if b || eat(d) {
		println("  maybe ate")
	}
}

func ranges() int {
	c := mk(15)
	for i := range seq(3) {
		d := mk(16 + i)
		if i == 1 {
			consume(d)
			continue
		}
		if i == 2 {
			consume(c)
			return i
		}
	}
	return -1
}

func gotos() {
	n := 0
again:
	c := mk(19 + n)
	n++
	if n < 2 {
		consume(c)
		goto again
	}
	println("  last", c.id)
}

func closures() {
	c := mk(22)
	set := func() { c = mk(23) }
	consume(c)
	set() // c holds 23 again, which the end of its scope drops
}

func pointers() {
	c := mk(24)
	p := &c
	*p = mk(25) // drops 24, through the view
	println("  through pointer", p.id)
	consume(c) // the last use of p was before the move (section 18.3)
}

func deferredReset() {
	c := mk(26)
	defer c.Reset(27)
	consume(c)
}

func selects(ch chan int) {
	c := mk(28)
	d := mk(29)
	select {
	case v := <-ch:
		println("  received", v)
		consume(c)
	default:
		consume(d)
	}
}

func main() {
	println("loops")
	loops()
	println("switches")
	for k := 0; k < 3; k++ {
		switches(k)
	}
	println("short circuit")
	shortCircuit(true)
	shortCircuit(false)
	println("range over func")
	println("  returned", ranges())
	println("goto")
	gotos()
	println("closure")
	closures()
	println("pointer")
	pointers()
	println("deferred method")
	deferredReset()
	println("select")
	ch := make(chan int, 1)
	ch <- 7
	selects(ch)
	selects(ch)

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
