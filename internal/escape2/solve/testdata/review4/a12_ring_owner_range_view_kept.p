package main

// A range over a ring of owners yields views of its elements.
type Conn owner struct{ id int }

func (c *Conn) Drop() {}

var kept *Conn

func f() {
	var r ring[Conn, 4]
	r.Push(Conn{id: 1})
	for c := range r.Values() {
		kept = c
	}
}

func main() { f() }
