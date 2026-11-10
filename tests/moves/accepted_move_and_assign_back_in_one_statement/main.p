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

func grow(b Buf) Buf { b.n++; return b }

// Assigning the result back to the variable a call moves is not a use of it,
// and a permutation moves each value once (language spec section 11.14).
func main() {
	v := Buf{}
	v = grow(v)
	a, b := Buf{}, Buf{}
	a, b = b, a
	n := v.Len() + a.Len()
	_ = n + take2(b)
	_ = v.Len() + a.Len()
}
