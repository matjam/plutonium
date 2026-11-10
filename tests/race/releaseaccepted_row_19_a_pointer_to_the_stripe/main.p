package main

import "pluto/sync"

var (
	stripes [4]sync.Mutex
	counts  [4]int
)

func bump(h int, done chan bool) {
	n := len(stripes)
	s := &stripes[h%n]
	s.Lock()
	counts[h%n]++
	s.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task bump(1, done)
	task bump(2, done)
	<-done
	<-done
}
