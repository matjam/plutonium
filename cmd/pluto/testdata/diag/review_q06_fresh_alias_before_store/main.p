package main

// q := p copied before the loop; the closure stored through fresh p in the loop; read through q.
type W struct{ f func() int }

func mk(x int) func() int { return func() int { return x * 10 } }



func main() {
	p := new(W)
	q := p
	for i := 0; i < 3; i++ {
		p.f = mk(i)
	}
	println(q.f())
}
