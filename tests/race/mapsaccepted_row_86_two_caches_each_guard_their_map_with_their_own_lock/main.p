package main

import "pluto/sync"

type Cache struct {
	mu sync.Mutex
	m  map[string]int
}

func (c *Cache) Put(k string, v int) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

func (c *Cache) Get(k string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[k]
}

var users = &Cache{m: map[string]int{}}
var hosts = &Cache{m: map[string]int{}}

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
	println(users.Get("ann"), hosts.Get("db"))
}
