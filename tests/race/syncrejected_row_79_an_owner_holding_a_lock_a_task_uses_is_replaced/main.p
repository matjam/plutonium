package main

import "pluto/sync"

type S struct{ mu sync.Mutex }

var b owned[S]

func user(mu *sync.Mutex, done chan bool) {
	mu.Lock()
	mu.Unlock()
	done <- true
}

func main() {
	b = new(S{})
	done := make(chan bool)
	task user(&b.mu, done)
	b = new(S{}) // frees the S whose mu the task uses
	<-done
}
