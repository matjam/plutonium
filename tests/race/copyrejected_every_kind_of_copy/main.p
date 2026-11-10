package main

import "pluto/sync"

type Counter struct {
	mu sync.Mutex
	n  int
}

type pair struct {
	c [2]Counter
}

var (
	total Counter
	all   [4]Counter
	group sync.WaitGroup
	once  sync.Once
	rw    sync.RWMutex
)

func show(c Counter) { println(c.n) }

func get(p *Counter) Counter {
	return *p // the result
}

func main() {
	var c Counter = total // the initialization
	c = all[1]            // the assignment
	show(total)           // the argument
	_ = get(&total)
	ch := make(chan Counter, 1)
	ch <- total // the send
	<-ch
	for _, x := range all { // the range value
		println(x.n)
	}
	var v any = total // a conversion to an interface
	_ = v
	p := pair{c: [2]Counter{total, all[0]}} // composite literal elements copied from places
	q := p                                  // through an array field
	_ = q
	g := group // a WaitGroup
	o := once  // a Once
	r := rw    // an RWMutex
	_, _, _ = g, o, r
	println(c.n)
}
