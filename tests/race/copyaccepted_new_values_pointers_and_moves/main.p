package main

import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

// Get has a pointer receiver, so a call copies nothing.
func (c *Counter) Get() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

type Box owner struct {
	c Counter
	b *int
}

func newCounter(n int) Counter { return Counter{n: n} }

var all [4]Counter

func main() {
	c := Counter{n: 1}       // a composite literal is a new value
	var z Counter            // a zero value
	d := newCounter(2)       // the result of a call is a new value
	p := &c                  // a pointer copies nothing
	for i := range all {     // range over the indexes
		println(all[i].Get())
	}
	for _, q := range []*Counter{&c, &z, &d} {
		println(q.Get())
	}
	b := Box{c: Counter{n: 3}}
	b2 := b // moving an owner is a move
	println(p.Get(), b2.c.n)
}
