package main

// A pointer-receiver method may be called on a temporary of owner type,
// such as the result of a call (spec section 11.14). The temporary itself is
// the receiver; it is dropped at the end of the statement, or of the whole
// if or switch statement whose header makes it (section 18.6), and a view
// of it may be passed to calls in that statement.

type Buf owner struct {
	data [4]int
	n    int
}

func (b *Buf) Drop() {
	if b.n != 0 {
		println("drop", b.n)
	}
}

func (b *Buf) Slice() []int { return b.data[:b.n] }

func (b *Buf) Grow() *Buf {
	b.data[b.n] = 10 * (b.n + 1)
	b.n++
	return b
}

func (b Buf) Len() int { return b.n }

func readAll(n int) Buf {
	var b Buf
	for i := 0; i < n; i++ {
		b.data[i] = i + 1
	}
	b.n = n
	return b
}

func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}

func main() {
	println(sum(readAll(3).Slice()))
	println(len(readAll(2).Slice()), sum(readAll(1).Slice()))
	// The method changes the temporary itself, not a copy.
	println(sum(readAll(1).Grow().Grow().Slice()))
	// A value receiver takes the temporary.
	println(readAll(2).Len())

	if s := readAll(3).Slice(); len(s) > 0 {
		println("if", s[0], s[2])
	}
	switch s := readAll(2).Slice(); len(s) {
	case 2:
		println("switch", s[1])
	}
	for _, v := range readAll(2).Slice() {
		println("range", v)
	}
	println("end")
}
