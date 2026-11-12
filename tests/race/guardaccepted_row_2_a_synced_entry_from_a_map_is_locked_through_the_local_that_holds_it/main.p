package main

import (
	"pluto/alloc"
	"pluto/sync"
)

type Entry struct {
	mu   sync.Mutex
	hits int
}

var mu sync.Mutex
var cache = map[string]synced[Entry]{}

func get(k string) synced[Entry] {
	mu.Lock()
	e, ok := cache[k]
	if !ok {
		e = alloc.Synced[Entry]()
		cache[k] = e
	}
	mu.Unlock()
	return e
}

func hit(k string, done chan bool) {
	e := get(k)
	e.mu.Lock()
	e.hits++
	e.mu.Unlock()
	done <- true
}

func main() {
	done := make(chan bool)
	task hit("a", done)
	task hit("a", done)
	<-done
	<-done
	e := get("a")
	e.mu.Lock()
	println(e.hits)
	e.mu.Unlock()
}
