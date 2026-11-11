package main

import (
	"pluto/alloc"
	"pluto/sync"
)

// The twin of the accepted test: Take changes next after it unlocks, so the
// field is not guarded by the object's lock and a view of the object may
// not cross (language spec section 19.3, Rule 3).

type Pages interface {
	Take(n int) int
}

type slab struct {
	mu   sync.Mutex
	next int
}

func (s *slab) Take(n int) int {
	s.mu.Lock()
	p := s.next
	s.mu.Unlock()
	s.next = p + n
	return p
}

var def Pages

func worker(done chan int) {
	done <- def.Take(4)
}

func use(p Pages, done chan int) {
	done <- p.Take(8)
}

func main() {
	def = alloc.New[slab]()
	done := make(chan int, 4)
	task worker(done)
	task worker(done)
	task use(def, done)
	task use(def, done)
	println(<-done + <-done + <-done + <-done)
}
