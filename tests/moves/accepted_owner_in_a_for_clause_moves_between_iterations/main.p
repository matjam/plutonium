package main

// An owner declared in a for clause moves to the next iteration and is
// dropped once, when the loop ends (language spec sections 12.10 and
// 18.6, D65). Each value next consumes is dropped there.

type Buf owner struct{ n int }

func (b *Buf) Drop() { println("drop", b.n) }

func next(b Buf) Buf { return Buf{n: b.n + 1} }

func main() {
	for b := (Buf{n: 0}); b.n < 3; b = next(b) {
		println(b.n)
	}
	println("end")
}
