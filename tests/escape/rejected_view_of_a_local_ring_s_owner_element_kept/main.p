package main

type Conn owner struct{ id int }
func (c *Conn) Drop() {}
var kept *Conn
func f() {
	var r ring[Conn, 4]
	r.Push(Conn{id: 1})
	if c, ok := r.Peek(); ok {
		kept = c
	}
}
func main() { f() }
