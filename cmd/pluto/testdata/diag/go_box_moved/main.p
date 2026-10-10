package main

type Server owner struct{ name string }

func (s *Server) Serve() { println("serving", s.name) }

// go p.Serve() moves the owned box into the new puroutine, which drops it
// when Serve returns, so p holds nothing afterwards.
func main() {
	p := new(Server)
	p.name = "files"
	go p.Serve()
	println(p.name)
}
