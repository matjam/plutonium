package main

import "pluto/sync"

func send(b []byte) { println(len(b)) }

func use(p *int) { println(*p) }

func main() {
	n := 1
	p := &n
	task use(p)
	var s sync.Scope
	s.Wait()
}
