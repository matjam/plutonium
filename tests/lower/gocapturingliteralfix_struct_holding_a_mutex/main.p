package main

import "pluto/sync"

func main() {
	var s struct{ mu sync.Mutex }
	task func() { s.mu.Lock() }()
}
