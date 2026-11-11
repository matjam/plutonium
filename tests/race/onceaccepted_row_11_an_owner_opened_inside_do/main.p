package main

import "pluto/sync"

type DB owner struct {
	n int
}

var (
	inst owned[DB]
	once sync.Once
)

func open() owned[DB] {
	return new(DB{n: 7})
}

func get() int {
	once.Do(func() { inst = open() })
	return inst.n
}

func worker(done chan bool) {
	println(get())
	done <- true
}

func main() {
	done := make(chan bool)
	task worker(done)
	task worker(done)
	<-done
	<-done
}
