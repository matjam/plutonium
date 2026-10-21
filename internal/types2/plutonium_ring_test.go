package types2_test

import "testing"

// TestRingOfOwnersViews checks that Peek, All and Values of a ring of owners
// yield views (*T) of the elements, and of other rings the elements
// themselves (language spec sections 7.17 and 12.10).
func TestRingOfOwnersViews(t *testing.T) {
	mustCheck(t, `package p

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
`)
	mustFail(t, `package p

type Conn owner struct{ id int }

func (c *Conn) Drop() {}

func copyOut(r *ring[Conn, 4]) {
	var c Conn
	c, _ = r.Peek()
	_ = c
}
`, "cannot use")
}
