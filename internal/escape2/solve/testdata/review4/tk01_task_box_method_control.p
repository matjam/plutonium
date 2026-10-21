package main

import (
	"pluto/sync"
)

type Server owner struct{ name string }

func (s *Server) Serve(wg *sync.WaitGroup) {
	println("serving", s.name)
	wg.Done()
}

var wg sync.WaitGroup

// task p.Serve() moves the owned box into the new task, which drops it when
// Serve returns: the task takes the box, not a view of this frame.
func main() {
	p := new(Server)
	p.name = "files"
	wg.Add(1)
	task p.Serve(&wg)
	wg.Wait()
}
