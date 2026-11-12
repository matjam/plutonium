package main

import "pluto/sync/atomic"

type Stats struct {
	requests atomic.Int64
	errors   atomic.Int64
}

var stats Stats
var pstats = &Stats{}

func work(done chan bool) {
	stats.requests.Add(1)
	pstats.requests.Add(1)
	pstats.errors.Add(1)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
	println(stats.requests.Load(), pstats.requests.Load(), pstats.errors.Load())
}
