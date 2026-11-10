package main

import "pluto/sync"

var (
	a, b  sync.Mutex
	stats [4]int
	count int
)

func add(v int, done chan bool) {
	a.Lock()
	b.Lock()
	stats[v] = v
	count++
	b.Unlock()
	a.Unlock()
	done <- true
}

func readA(done chan bool) {
	a.Lock()
	println(count)
	a.Unlock()
	done <- true
}

func readB(done chan bool) {
	b.Lock()
	println(count)
	b.Unlock()
	done <- true
}

// keep loads a view of stats under both locks and uses it after b is
// released, with a still held.
func keep(done chan bool) {
	a.Lock()
	b.Lock()
	s := stats[:]
	b.Unlock()
	for _, v := range s {
		println(v)
	}
	a.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task add(1, done)
	task add(2, done)
	task readA(done)
	task readB(done)
	task keep(done)
	for i := 0; i < 5; i++ {
		<-done
	}
}
