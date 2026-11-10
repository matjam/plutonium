package main

type Buf owner struct{ b []byte; n int }

func (b *Buf) Len() int { return b.n }
func (b Buf) Consume() int { return b.n }
func (b *Buf) Clone() Buf { return Buf{n: b.n} }

type File owner struct{ fd int }

func (f *File) Drop() {}

type Conn owner struct {
	name string
	buf  Buf
}

var global Buf

func take2(b Buf) int { return b.n }
func look(b *Buf) int { return b.n }
func cond() bool { return true }

// The direct call and the method expression of a consuming method are
// allowed; only the method value is an error (section 11.9).
func main() {
	m := Buf{}
	_ = m.Consume()
	consume := Buf.Consume
	_ = consume(Buf{})
	_ = Buf.Consume(Buf{n: 1})
	p := Buf{}
	l := p.Len
	_ = l()
}
