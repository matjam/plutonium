package main

// A view of an owner temporary that a pointer method is called on may be
// passed to calls in the statement but may not be kept (spec section
// 11.14): the temporary is dropped at the end of the statement, or of the
// whole if or switch statement whose header makes it.

type Buf owner struct {
	data [4]int
	n    int
}

func (b *Buf) Slice() []int { return b.data[:b.n] }

func readAll(n int) Buf {
	var b Buf
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

type Holder struct{ s []int }

var keep []int

func main() {
	println(sum(readAll(3).Slice()))         // allowed: passed to a call
	if s := readAll(3).Slice(); len(s) > 0 { // allowed: s ends with the if statement
		println(s[0])
	}

	s := readAll(3).Slice()
	println(len(s))
	keep = readAll(2).Slice()
	var i any = readAll(1).Slice()
	_ = i
	h := &Holder{s: readAll(1).Slice()}
	_ = h
	for v := readAll(2).Slice(); len(v) > 0; v = v[1:] {
		println(v[0])
	}
}

func get() []int {
	return readAll(2).Slice()
}
