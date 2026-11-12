package main

import "pluto/sync"

type Entry struct{ hits int }

var mu sync.Mutex
var cache = map[string]*Entry{}

func get(k string) int {
	mu.Lock()
	defer mu.Unlock()
	e := cache[k]
	if e == nil {
		e = &Entry{}
		cache[k] = e
	}
	return e.hits
}

func work(done chan bool) {
	get("a")
	done <- true
}

func main() {
	done := make(chan bool)
	task work(done)
	task work(done)
	<-done
	<-done
}
