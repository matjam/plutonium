package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Config struct{ port int }

var (
	mu  sync.Mutex
	cfg synced[Config]
)

func current() synced[Config] {
	mu.Lock()
	c := cfg
	mu.Unlock()
	return c
}

func reload(done chan bool) {
	c := alloc.Synced[Config]()
	mu.Lock()
	cfg = c
	mu.Unlock()
	done <- true
}

func reader(done chan bool) {
	c := current()
	println(c.port)
	done <- true
}

func main() {
	cfg = alloc.Synced[Config]()
	done := make(chan bool)
	task reload(done)
	task reader(done)
	task reader(done)
	<-done
	<-done
	<-done
}
