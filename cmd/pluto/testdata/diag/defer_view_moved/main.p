package main

// A deferred call keeps the arguments it was given and runs when the
// function returns (spec sections 18.3 and 18.5): a view of an owner among
// them, such as its address, is used after the owner moved if the function
// moves the owner later. Copies of what the owner holds are fine.

type Buf owner struct{ n int }

func (b *Buf) Len() int { return b.n }

func consume(b Buf) int { return b.n }

func report(b *Buf) { println(b.Len()) }

func number(n int) { println(n) }

func moved() {
	o := Buf{n: 1}
	defer report(&o)
	consume(o)
}

func maybe(c bool) {
	o := Buf{n: 2}
	defer report(&o)
	if c {
		consume(o)
	}
}

func copied() {
	o := Buf{n: 3}
	defer number(o.n)
	consume(o)
}

func main() {
	moved()
	maybe(true)
	copied()
}
