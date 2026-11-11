package main

import "pluto/sync"

type Registry struct {
	names [4]string
	n     int
}

var (
	regOnce sync.Once
	reg     Registry
)

func registry() *Registry {
	regOnce.Do(func() {
		reg.names[0] = "a"
		reg.names[1] = "b"
		reg.n = 2
	})
	return &reg
}

func worker(done chan bool) {
	r := registry()
	println(r.n, r.names[0])
	done <- true
}

func main() {
	done := make(chan bool)
	task worker(done)
	task worker(done)
	<-done
	<-done
}
