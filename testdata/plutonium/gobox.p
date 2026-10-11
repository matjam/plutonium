package main

// task p.Serve() on a box (language spec section 4.7): the new task
// takes the box itself. An owned[T] moves into it, so the task owns
// the object and drops it when the method returns; a synced[T] is counted
// for it, so the object lives until the last reference is gone, whichever
// task holds it last. A shared[T] cannot cross tasks (see
// cmd/pluto/testdata/diag/gobox_shared.p). Every Drop reports on a wait
// group, so main can wait for it; the program also runs under the debug
// allocator, which fails on a leak, with one, two and many processors.

import (
	"pluto/alloc"
	"pluto/rt"
	"pluto/sync"
)

// mu guards hits, which the tasks holding a synced Server change.
type Server owner struct {
	name string
	mu   sync.Mutex
	hits int
}

func (s *Server) Drop() {
	s.mu.Lock()
	println("drop", s.name, s.hits)
	s.mu.Unlock()
	dropped.Done()
}

func (s *Server) Serve() {
	s.mu.Lock()
	s.hits++
	s.mu.Unlock()
	println("serve", s.name)
	served.Done()
}

func (s *Server) Add(n int) {
	s.mu.Lock()
	s.hits += n
	s.mu.Unlock()
	println("add", s.name, n)
	served.Done()
}

// Leave ends its task with rt.Goexit: the box is dropped (or released)
// all the same.
func (s *Server) Leave() {
	println("leave", s.name)
	served.Done()
	rt.Goexit()
}

// served and dropped count the calls and drops main waits for. A drop may
// be main's own, when it releases the last reference itself.
var served, dropped sync.WaitGroup

func newServer(name string) owned[Server] {
	s := new(Server)
	s.name = name
	return s
}

type Farm owner struct {
	main synced[Server]
	n    int
}

// expect adds a call and a drop for main to wait for.
func expect() {
	served.Add(1)
	dropped.Add(1)
}

func main() {
	// An owned box moves into the task, which drops it.
	p := newServer("owned")
	expect()
	task p.Serve()
	served.Wait()
	dropped.Wait()

	// A temporary box moves too, and so do arguments.
	expect()
	task newServer("temporary").Add(5)
	served.Wait()
	dropped.Wait()

	// The task drops the box even when the method ends it with Goexit.
	q := newServer("owned-leave")
	expect()
	task q.Leave()
	served.Wait()
	dropped.Wait()

	// A synced box is counted for each task: main keeps its own
	// reference, and the object is dropped when the last one goes.
	y := alloc.Synced[Server]()
	y.name = "synced"
	for i := 0; i < 3; i++ {
		served.Add(1)
		task y.Serve()
		served.Wait()
	}
	expect()
	task y.Add(10)
	served.Wait()
	y = nil
	dropped.Wait()

	// A reference in a field is copied, not moved out.
	f := Farm{main: alloc.Synced[Server]()}
	f.main.name = "farm"
	expect()
	task f.main.Serve()
	served.Wait()
	println("farm still holds", f.main.name, f.n)
	f.main = nil
	dropped.Wait()

	z := alloc.Synced[Server]()
	z.name = "synced-leave"
	expect()
	task z.Leave()
	served.Wait()
	z = nil
	dropped.Wait()

	// Drop reports before the memory is freed, and nothing in the program
	// follows the task's own release: main's return waits for a drop
	// another task has begun (language spec section 15.4), so the
	// leak check at exit sees the box freed.
	println("end")
}
