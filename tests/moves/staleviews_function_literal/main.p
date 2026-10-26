package main

type Buf owner struct{ b []byte; n int }

func (b *Buf) Len() int    { return b.n }
func (b *Buf) Me() *Buf    { return b }
func (b *Buf) Bytes() []byte { return b.b }
func (b Buf) Consume() int { return b.n }
func (b *Buf) Clone() Buf  { return Buf{n: b.n} }

type H struct {
	p *Buf
	n int
}

type V owner struct{ p *Buf }

func take2(b Buf) int          { return b.n }
func look(b *Buf) int          { return b.n }
func first(b *Buf) *Buf        { return b }
func link(h *H, b *Buf)        { h.p = b }
func store(pp **Buf, b *Buf)   { *pp = b }
func cond() bool               { return true }

func main() {
	b := Buf{}
	p := &b
	f := func() int { return p.n }
	take2(b)
	_ = f()
}
