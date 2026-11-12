package cache

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

var Users = &Cache{m: map[string]int{}}
var Hosts = &Cache{m: map[string]int{}}
