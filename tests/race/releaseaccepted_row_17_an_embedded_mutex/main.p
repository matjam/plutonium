package main

import "pluto/sync"

type counter struct {
	sync.Mutex
	n int
}

var c counter

func inc(c *counter, done chan bool) {
	c.Lock()
	c.n++
	c.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task inc(&c, done)
	inc(&c, done)
	<-done
	<-done
}
