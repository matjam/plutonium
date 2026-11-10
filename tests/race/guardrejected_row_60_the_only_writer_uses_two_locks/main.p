package main

import "pluto/sync"

var (
	l, m sync.Mutex
	x    int
)

// writer is the only writer of x, but holds l at one write and m at the
// other, so no lock guards x.
func writer(done chan bool) {
	l.Lock()
	x = 1
	l.Unlock()
	m.Lock()
	x = 2
	m.Unlock()
	done <- true
}

func reader(done chan bool) {
	l.Lock()
	println(x)
	l.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task writer(done)
	task reader(done)
	<-done
	<-done
}
