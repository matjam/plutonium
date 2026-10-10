package main

// Using owners after they moved (language spec sections 6.4, 6.9).

type Buf owner struct {
	b []byte
	n int
}

func (b *Buf) Len() int    { return b.n }
func (b *Buf) Clone() Buf  { return Buf{n: b.n} }
func (b Buf) Consume() int { return b.n }

type Tree owner struct {
	left Buf
}

func flush(b Buf) {}

func verbose() bool { return true }

func main() {
	m := Buf{n: 1}
	n := m
	println(m.Len(), n.Len())

	b := Buf{}
	if verbose() {
		flush(b)
	}
	println(b.Len())

	var t Tree
	left := t.left
	_ = left

	out := Buf{}
	for i := 0; i < 3; i++ {
		flush(out)
	}

	c := Buf{}
	do := func() { flush(c) }
	do()
}
