package main

import (
	"pluto/fmt"
	"pluto/sync"
)

type Cache struct {
	mu sync.RWMutex
	m  map[string]int
}

func (c *Cache) Get(k string) (int, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.m[k]
	return v, ok
}

func (c *Cache) Put(k string, v int) {
	c.mu.Lock()
	c.m[k] = v
	c.mu.Unlock()
}

func main() {
	c := Cache{m: map[string]int{}}
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() { c.Put(fmt.Sprint("k", i), i*10) })
	}
	s.Wait()
	v, ok := c.Get("k3")
	fmt.Println(v, ok)
	_, ok = c.Get("zz")
	fmt.Println(ok)
}
