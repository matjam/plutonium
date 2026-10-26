package main

var global int

type S struct {
	n int
	q synced[int]
}

func f(p *int)       {}
func h(s S, c chan S) {}

func g(p *int, c chan *int, s chan synced[int]) {
	task f(p)
	task f(&global)
	c <- p
	task h(S{}, make(chan S, 1))
	select {
	case v := <-c:
		_ = v
	case c <- p:
	default:
	}
	for v := range c {
		_ = v
	}
	close(c)
	_, _ = len(c), cap(c)
}
