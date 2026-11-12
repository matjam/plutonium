package main

import (
	"pluto/rt"
	"unsafe"
)

var (
	n int
	t rt.Timer
)

// fire is the function of an rt.Timer: unsafe code, not a party.
func fire(arg unsafe.Pointer, now int64) { n++ }

func worker(done chan bool) {
	n++
	done <- true
}

func main() {
	t.Init(fire, nil)
	t.Reset(rt.Now(), 0)
	done := make(chan bool)
	task worker(done)
	<-done
	t.Stop()
}
