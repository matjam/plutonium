package main

import "pluto/sync/atomic"

type Stats struct {
	name string
	hits atomic.Int64
}

var st = &Stats{name: "x"}

func work(done chan bool) {
	st.hits.Add(1)
	println(st.name, st.hits.Load())
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
