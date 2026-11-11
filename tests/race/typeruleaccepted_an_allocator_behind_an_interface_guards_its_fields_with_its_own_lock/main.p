package main

import (
	"pluto/alloc"
	"pluto/sync"
)

// The shape of an allocator installed as the default: every task reaches
// it through an interface in a package variable, and its methods guard its
// fields with its own lock (language spec section 19.3, Rule 3). The lock
// is named relative to the receiver, so it guards the fields however the
// object is reached.

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
	s.next += n
	s.mu.Unlock()
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
