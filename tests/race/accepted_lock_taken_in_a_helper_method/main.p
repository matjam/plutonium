package main
import "pluto/sync"

type State struct {
	mu    sync.Mutex
	items [16]int
	n     int
}

var state State

func (s *State) lock()   { s.mu.Lock() }
func (s *State) unlock() { s.mu.Unlock() }

func (s *State) add(v int) {
	s.lock()
	defer s.unlock()
	if s.n < len(s.items) {
		s.items[s.n] = v
		s.n++
	}
}

func worker(v int, done chan bool) {
	state.add(v)
	done <- true
}

func main() {
	done := make(chan bool)
	for i := 0; i < 4; i++ {
		task worker(i, done)
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	state.lock()
	println(state.n)
	state.unlock()
}