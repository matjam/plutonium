package main

import "pluto/alloc"

type Server owner struct{ name string }

func (s *Server) Serve() { println("serving", s.name) }

// A shared box counts its references without atomics, so it cannot be the
// receiver of a go statement. A synced one can.
func main() {
	z := alloc.Shared[Server]()
	task z.Serve()

	y := alloc.Synced[Server]()
	task y.Serve()
}
