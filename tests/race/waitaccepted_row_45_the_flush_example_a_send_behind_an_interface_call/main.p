package main

import "pluto/rt"

type Sink interface{ Put(n int) }

type chanSink struct{ ch chan int }

func (s chanSink) Put(n int) { s.ch <- n }

func flush(s Sink, n int) {
	rt.DisablePreemption()
	s.Put(n) // compiles: a call through an interface is not followed
	rt.EnablePreemption()
}

func main() {
	s := chanSink{ch: make(chan int, 1)}
	flush(s, 1)
	println(<-s.ch)
}
