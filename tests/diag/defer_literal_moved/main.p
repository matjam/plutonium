package main

// A deferred function literal that uses an owner runs when the function
// returns (spec section 18.5): moving or dropping the owner later in the
// function is a use after a move, found at compile time.

type Buf owner struct{ n int }

func (b *Buf) Len() int { return b.n }

func consume(b Buf) int { return b.n }

func report(b *Buf) { println(b.Len()) }

func moved() {
	o := Buf{n: 1}
	defer func() { report(&o) }()
	consume(o)
}

func maybe(c bool) {
	o := Buf{n: 2}
	defer func() { report(&o) }()
	if c {
		consume(o)
	}
}

func main() {
	moved()
	maybe(true)
}
