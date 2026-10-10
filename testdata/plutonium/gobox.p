package main

// go p.Serve() on a box (language spec section 4.7): the new puroutine
// takes the box itself. An owned[T] moves into it, so the puroutine owns
// the object and drops it when the method returns; a synced[T] is counted
// for it, so the object lives until the last reference is gone, whichever
// puroutine holds it last. A shared[T] cannot cross puroutines (see
// cmd/pluto/testdata/diag/gobox_shared.p). Every Drop reports on a channel,
// so main can wait for it; the program also runs under the debug
// allocator, which fails on a leak, with one, two and many processors.

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync"
	"pluto/time"
)

// mu guards hits, which the puroutines holding a synced Server change.
type Server owner struct {
	name string
	mu   sync.Mutex
	hits int
}

func (s *Server) Drop() {
	s.mu.Lock()
	println("drop", s.name, s.hits)
	s.mu.Unlock()
	dropped <- true
}

func (s *Server) Serve() {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	println("serve", s.name)
	served <- true
}

func (s *Server) Add(n int) {
	s.mu.Lock()
	s.hits += n
	s.mu.Unlock()
	println("add", s.name, n)
	served <- true
}

// Leave ends its puroutine with rt.Goexit: the box is dropped (or released)
// all the same.
func (s *Server) Leave() {
	println("leave", s.name)
	served <- true
	rt.Goexit()
}

var served, dropped chan bool

func newServer(name string) owned[Server] {
	s := new(Server)
	s.name = name
	return s
}

type Farm owner struct {
	main synced[Server]
	n    int
}

func main() {
	served = make(chan bool)
	dropped = make(chan bool, 1) // main may drop the last reference itself

	// An owned box moves into the puroutine, which drops it.
	p := newServer("owned")
	go p.Serve()
	<-served
	<-dropped

	// A temporary box moves too, and so do arguments.
	go newServer("temporary").Add(5)
	<-served
	<-dropped

	// The puroutine drops the box even when the method ends it with Goexit.
	q := newServer("owned-leave")
	go q.Leave()
	<-served
	<-dropped

	// A synced box is counted for each puroutine: main keeps its own
	// reference, and the object is dropped when the last one goes.
	y := alloc.Synced[Server]()
	y.name = "synced"
	for i := 0; i < 3; i++ {
		go y.Serve()
		<-served
	}
	go y.Add(10)
	<-served
	y = nil
	<-dropped

	// A reference in a field is copied, not moved out.
	f := Farm{main: alloc.Synced[Server]()}
	f.main.name = "farm"
	go f.main.Serve()
	<-served
	println("farm still holds", f.main.name, f.n)
	f.main = nil
	<-dropped

	z := alloc.Synced[Server]()
	z.name = "synced-leave"
	go z.Leave()
	<-served
	z = nil
	<-dropped

	// Drop reports before the memory is freed, and nothing in the program
	// follows the puroutine's own release, so give the last puroutine time
	// to finish before the leak check at exit.
	time.Sleep(100 * time.Millisecond)
	alloc.FreeChan(served)
	alloc.FreeChan(dropped)
	println("end")
}
