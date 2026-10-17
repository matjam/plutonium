package main

// Deferred calls and owners (language spec section 12.19): the function
// value, the arguments and the receiver are evaluated at the defer
// statement. An owner passed by value, or the receiver of a value-receiver
// method, moves there; a pointer-receiver method saves the address and the
// variable stays usable; defer x.Drop() drops whatever x holds when the
// deferred call runs, and x is not dropped again at the end of its scope.
// The program also runs under the debug allocator, which fails on a leak.

type Conn owner struct{ id int }

func mk(id int) Conn { return Conn{id: id} }

func (c *Conn) Drop() {
	if c.id == 0 {
		println("  drop empty")
		return
	}
	println("  drop", c.id)
}

func (c *Conn) Use() int { return c.id }

func (c *Conn) Show() { println("  show", c.id) }

// Close consumes its receiver, which it drops when it returns.
func (c Conn) Close() { println("  close", c.id) }

func consume(c Conn) { println("  consume", c.id) }

// byValue: the argument moves at the defer statement; nothing is dropped at
// the end of the scope, and the call consumes the owner after the body.
func byValue(id int) {
	c := mk(id)
	defer consume(c)
	println("  body")
}

// valueReceiver: the receiver moves at the defer statement.
func valueReceiver(id int) {
	c := mk(id)
	defer c.Close()
	println("  body")
}

// earlyDrop: the owner stays usable after defer x.Drop(), and is dropped
// once, when the deferred call runs.
func earlyDrop(id int) {
	c := mk(id)
	defer c.Drop()
	println("  use", c.Use())
	println("  body")
}

// movedAfter: the owner is moved on after defer x.Drop(), so the deferred
// call finds nothing (the zero value) to drop.
func movedAfter(id int) {
	c := mk(id)
	defer c.Drop()
	consume(c)
	println("  body")
}

// reassigned: the deferred call drops what the variable holds when it runs,
// the value of the second assignment; the first value is dropped by the
// assignment.
func reassigned(id int) {
	c := mk(id)
	defer c.Drop()
	c = mk(id + 1)
	println("  body", c.Use())
}

// savedAddress: a pointer-receiver method saves &c, so the call sees the
// value the variable holds when it runs, and the scope end drops it once.
func savedAddress(id int) {
	c := mk(id)
	defer c.Show()
	c = mk(id + 1)
	println("  body", c.Use())
}

// order: deferred calls run in reverse order, before the variables are
// dropped.
func order(id int) {
	a := mk(id)
	b := mk(id + 1)
	defer a.Close()
	defer b.Drop()
	println("  body")
}

func main() {
	println("by value")
	byValue(1)
	println("value receiver")
	valueReceiver(2)
	println("early drop")
	earlyDrop(3)
	println("moved after")
	movedAfter(4)
	println("reassigned")
	reassigned(5)
	println("saved address")
	savedAddress(9)
	println("order")
	order(7)
}
