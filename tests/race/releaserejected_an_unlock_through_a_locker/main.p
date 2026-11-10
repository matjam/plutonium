package main

import "pluto/sync"

var (
	mu sync.Mutex
	n  int
)

func withLocker(l sync.Locker, done chan bool) {
	l.Lock()
	n++
	l.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task withLocker(&mu, done)
	<-done
}
