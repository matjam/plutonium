package main

type Conn owner struct{ id int }
var last *Conn
func (c *Conn) Drop() { last = c }
func f() {
	var r ring[Conn, 4]
	r.Push(Conn{id: 1})
	r.Reset()
}
func main() { f() }
