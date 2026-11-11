package main

// An owner declared in a for clause carries over to the next iteration
// (language spec sections 12.10 and 18.6, D65), so moving it out without
// giving it a new value leaves the next iteration nothing to use (E3301).

type Buf owner struct{ n int }

func (b *Buf) Drop() { println("drop", b.n) }

func consume(b Buf) {}

func main() {
	for b := (Buf{n: 0}); b.n < 3; consume(b) {
		println(b.n)
	}
	for c := (Buf{n: 0}); c.n < 3; c.n++ {
		consume(c)
	}
}
