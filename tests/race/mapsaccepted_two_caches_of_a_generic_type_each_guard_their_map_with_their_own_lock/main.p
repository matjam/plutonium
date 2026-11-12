package main

import "pluto/sync"

type Cache[V any] struct {
	mu sync.Mutex
	m  map[string]V
}

func (c *Cache[V]) Put(k string, v V) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

var users = &Cache[int]{m: map[string]int{}}
var hosts = &Cache[int]{m: map[string]int{}}

func work(done chan bool) {
	users.Put("ann", 1)
	hosts.Put("db", 2)
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
