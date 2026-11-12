package main

import "pluto/sync"

type Config struct{ port int }

var (
	mu  sync.Mutex
	cfg owned[Config]
)

func reload(done chan bool) {
	mu.Lock()
	cfg = new(Config{port: 81}) // frees the Config a reader may still use
	mu.Unlock()
	done <- true
}

func reader(done chan bool) {
	mu.Lock()
	c := &*cfg
	mu.Unlock()
	println(c.port)
	done <- true
}

func main() {
	cfg = new(Config{port: 80})
	done := make(chan bool)
	task reload(done)
	task reader(done)
	<-done
	<-done
}
