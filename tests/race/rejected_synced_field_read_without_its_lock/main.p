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

func peek(s synced[Stats], done chan bool) {
	println(s.hits)
	done <- true
}

func main() {
	var s synced[Stats]
	done := make(chan bool)
	task record(s, done)
	task peek(s, done)
	<-done
	<-done
}