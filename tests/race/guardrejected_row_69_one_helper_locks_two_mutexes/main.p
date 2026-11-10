package main

import "pluto/sync"

var (
	a, b sync.Mutex
	x    int
)

func bump(l *sync.Mutex) {
	l.Lock()
	x++
	l.Unlock()
}

func withA(done chan bool) {
	bump(&a)
	done <- true
}

func withB(done chan bool) {
	bump(&b)
	done <- true
}

func main() {
	done := make(chan bool)
	task withA(done)
	task withB(done)
	<-done
	<-done
}
