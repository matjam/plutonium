package main

type Conn owner struct{ id int }

func (c *Conn) Drop() {}

func copyOut(r *ring[Conn, 4]) {
	var c Conn
	c, _ = r.Peek()
	_ = c
}
