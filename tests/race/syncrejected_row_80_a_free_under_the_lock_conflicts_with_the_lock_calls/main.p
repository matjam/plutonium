package main

import "pluto/sync"

type S struct{ mu sync.Mutex }

var b owned[S]

func user(done chan bool) {
	b.mu.Lock()
	b.mu.Unlock()
	done <- true
}

func replacer(done chan bool) {
	b.mu.Lock()
	b = new(S{}) // frees the S whose mu the other task locks
	b.mu.Unlock()
	done <- true
}

func main() {
	b = new(S{})
	done := make(chan bool)
	task user(done)
	task replacer(done)
	<-done
	<-done
}
