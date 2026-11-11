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

func (c *Cache) Reset() {
	c.mu.Lock()
	m := make(map[string]int)
	m["a"] = 1
	c.m = m
	c.mu.Unlock()
}

func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.m)
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
	users.Reset()
	println(users.Len(), hosts.Len())
}
