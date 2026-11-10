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

type Pair owner struct {
	n int
	b Buf
}

func send2(n int, b Buf) {}
func (b *Buf) Absorb(o Buf) {}

// Each line moves a variable and uses it in another part of the same
// expression or statement, with the use first. Both orders are errors
// (language spec sections 11.14 and 18.5).
func main() {
	a := Buf{}
	send2(a.Len(), a)

	b := Buf{}
	_ = b.Len() + take2(b)

	c := Buf{}
	c.Absorb(c)

	d := Buf{}
	p := Pair{n: d.Len(), b: d}
	_ = p

	var xs [4]int
	e := Buf{}
	_ = xs[e.Len()] + take2(e)

	f := Buf{}
	s := xs[:]
	_ = s[f.Len():take2(f)]

	g := Buf{}
	xs[g.Len()] = take2(g)

	h := Buf{}
	send2(func() int { return h.Len() }(), h)

	var chs [2]chan Buf
	i := Buf{}
	select {
	case chs[i.Len()] <- i:
	default:
	}
}
