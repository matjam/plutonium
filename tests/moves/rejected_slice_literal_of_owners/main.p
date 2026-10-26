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

func main() {
	b := Buf{}
	s := []Buf{b}
	_ = s
}
