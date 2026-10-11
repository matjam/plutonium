package main

type Server owner struct{ name string }

func (s *Server) Serve() { println("serving", s.name) }

// task p.Serve() moves the owned box into the new task, which drops it
// when Serve returns, so p holds nothing afterwards.
func main() {
	p := new(Server)
	p.name = "files"
	task p.Serve()
	println(p.name)
}
