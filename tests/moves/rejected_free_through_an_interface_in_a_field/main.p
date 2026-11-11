package main

import "pluto/vec"

// A method called through an ordinary interface that a field of the input
// holds may free what the interface points to, which may be memory of the
// caller's owner: the call frees what every method it may reach frees of
// its receiver (language spec section 18.3, 7.9).

type Pusher interface{ Push(x int) }

type Box struct{ p Pusher }

func kick(b *Box) { b.p.Push(1) }

func main() {
	var g vec.Vec[int]
	g.Push(0)
	gs := g.Slice()
	b := Box{p: &g}
	kick(&b)
	println(gs[0])
}
