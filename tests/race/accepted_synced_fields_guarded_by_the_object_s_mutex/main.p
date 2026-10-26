package main
import "pluto/sync"

type Stats struct {
	mu   sync.Mutex
	hits int
}

func record(s synced[Stats], done chan bool) {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	done <- true
}

func main() {
	var s synced[Stats]
	done := make(chan bool)
	task record(s, done)
	task record(s, done)
	<-done
	<-done
	s.mu.Lock()
	println(s.hits)
	s.mu.Unlock()
}