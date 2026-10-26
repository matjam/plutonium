package main

type Conn owner struct{ id int }

func (c *Conn) Drop() {}

func views(r *ring[Conn, 4], q *ring[int, 4]) {
	var p *Conn
	p, _ = r.Peek()
	for i, c := range r {
		p = c
		_ = i
	}
	for c := range r.Values() {
		p = c
	}
	_ = p
	var n int
	n, _ = q.Peek()
	for _, v := range q {
		n = v
	}
	_ = n
}
