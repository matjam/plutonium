package main

// A function literal that uses an owner, held in a variable and deferred,
// runs when the function returns (spec section 18.5): moving the owner later
// in the function is a use after a move. The compiler follows the variable
// and its copies without regard to the order of statements.

type Buf owner struct{ n int }

func (b *Buf) Len() int { return b.n }

func consume(b Buf) int { return b.n }

func moved() {
	o := Buf{n: 1}
	f := func() { println(o.Len()) }
	defer f()
	consume(o)
}

func alias(c bool) {
	o := Buf{n: 2}
	f := func() { println(o.Len()) }
	g := f
	defer g()
	if c {
		consume(o)
	}
}

func main() {
	moved()
	alias(true)
}
