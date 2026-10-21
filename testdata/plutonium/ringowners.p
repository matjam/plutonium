// A ring of owners (language spec sections 7.17 and 12.10): Peek, All and
// Values yield views (*T) of the elements, Pop moves the oldest one out,
// Reset drops the elements it holds, and dropping the ring drops the rest.
package main

import "pluto/vec"

type Conn owner struct {
	id  int
	buf vec.Vec[int] // heap memory, so a missed drop is a leak
}

func (c *Conn) Drop() { println("drop", c.id, c.buf.Len()) }

func open(id int) Conn {
	c := Conn{id: id, buf: vec.New[int]()}
	c.buf.Push(id)
	return c
}

func views() {
	var r ring[Conn, 4]
	r.Push(open(1))
	r.Push(open(2))
	for i, c := range r {
		c.buf.Push(i) // a view: the element in the ring changes
		println("all", i, c.id, c.buf.Len())
	}
	for c := range r.Values() {
		println("values", c.id, c.buf.Len())
	}
	if c, ok := r.Peek(); ok {
		c.id = 10
	}
	if c, ok := r.Pop(); ok {
		println("popped", c.id)
	} // the popped Conn is dropped here
	println("leaving views")
} // the ring drops Conn 2

func reset() {
	var r ring[Conn, 2]
	r.Push(open(3))
	r.Push(open(4))
	r.Reset() // drops 3 and 4
	println("after reset", r.Len())
	r.Push(open(5))
	if _, ok := r.Peek(); !ok {
		println("empty")
	}
	println("leaving reset")
} // the ring drops Conn 5

func main() {
	views()
	reset()
	println("end")
}
