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

func keep(f File) {}

// defer x.Drop() on a whole owner variable is not a use at the exit: a
// later move is allowed, and the deferred drop then drops the empty
// variable (language spec sections 12.19, 18.5 and 18.6).
func passed() {
	f := File{}
	defer f.Drop()
	keep(f)
}

func taken() {
	f := File{}
	defer f.Drop()
	g := take(&f)
	keep(g)
}

func onSomePaths() {
	f := File{}
	defer f.Drop()
	if cond() {
		keep(f)
	}
}

func main() {
	passed()
	taken()
	onSomePaths()
}
