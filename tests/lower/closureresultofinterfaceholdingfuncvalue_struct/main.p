package main

type Node interface{ Pos() int }
type Ident struct{ n int }

func (i *Ident) Pos() int { return i.n }

var one = &Ident{1}

type P struct{}

func (p *P) ident() Node { return one }

type Holder struct{ n Node }

func call(f func() Node) Node { return f() }

func build(p *P, h *Holder) *Holder {
	var local Holder
	local.n = call(p.ident)
	*h = local
	return h
}

func main() {
	var h Holder
	println(build(&P{}, &h).n.Pos())
}
type V struct{ f func() int }
func (v V) Pos() int { return v.f() }
