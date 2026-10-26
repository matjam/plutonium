package main
import (
	"pluto/alloc"
	"pluto/sync"
)

type S struct {
	mu   sync.Mutex
	hits int
}

func (s *S) Add(done chan bool) {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	done <- true
}

type F struct{ main synced[S] }

func main() {
	done := make(chan bool)
	f := F{main: alloc.Synced[S]()}
	task f.main.Add(done)
	f.main = nil
	<-done
}