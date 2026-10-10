// Built-in ring methods reached other than by a direct call: through an
// interface, a type parameter, a method value or expression, a defer, and
// promoted from a named ring type embedded in a struct.
package main

type Pusher interface {
	Push(int) bool
	Len() int
}

type Buf ring[int, 4]

type R4 = ring[int, 4]

type Q struct {
	n int
	Buf
}

func push[R Pusher](r R) bool { return r.Push(3) && r.Len() > 0 }

func drain[E any, R interface{ Pop() (E, bool) }](r R) int {
	n := 0
	for {
		_, ok := r.Pop()
		if !ok {
			return n
		}
		n++
	}
}

func main() {
	var r ring[int, 4]
	var p Pusher = &r
	println(p.Push(1), p.Len())
	f := r.Push
	println(f(2), r.Len())
	g := (*R4).Len
	println(g(&r))
	defer println("deferred len", r.Len())
	defer r.Reset()
	m := p.Len
	println(m())
	var b Buf
	println(b.Push(5), b.Len())
	q := &Q{}
	println(q.Push(7), q.Push(8), q.Len(), push(q))
	var pq Pusher = q
	println(pq.Push(9), pq.Len(), drain[int](q), q.Len())
	println(drain[int](&r))
}
