package main

// A pointer-receiver method may be called on a temporary of owner type for
// the duration of the statement (spec section 11.14), but not on a
// temporary of any other type, not as a method value, and not in a defer
// or task statement, whose call runs after the statement has dropped the
// temporary.

type Buf owner struct {
	data [4]int
	n    int
}

func (b *Buf) Slice() []int { return b.data[:b.n] }

func (b *Buf) Print() { println(b.n) }

func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}

type P struct{ x int }

func (p *P) Inc() int {
	p.x++
	return p.x
}

func mkP() P { return P{1} }

func main() {
	println(len(readAll(3).Slice())) // allowed
	println(mkP().Inc())
	f := readAll(1).Slice
	_ = f
	defer readAll(2).Print()
	task readAll(2).Print()
}
