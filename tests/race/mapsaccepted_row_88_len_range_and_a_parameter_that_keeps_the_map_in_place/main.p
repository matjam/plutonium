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

func count(m map[string]int) int { return len(m) }

func (c *Cache) Sum() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.m)
	for k := range c.m {
		n += c.m[k]
	}
	return n + count(c.m)
}

var users = &Cache{m: map[string]int{}}
var hosts = &Cache{m: map[string]int{}}

func work(done chan bool) {
	users.Put("ann", 1)
	hosts.Put("db", 2)
	println(users.Sum(), hosts.Sum())
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
	println(users.Sum(), hosts.Sum())
}
