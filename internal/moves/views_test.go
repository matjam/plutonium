package moves_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Views of local owners (views.go, language spec section 18.3).

const viewDecls = `
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
`

func TestStaleViews(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range []struct{ name, src, want string }{
		{"pointer", `
func main() {
	b := Buf{n: 1}
	p := &b
	take2(b)
	_ = p.n
}`, "p holds a view of b (&b, line 27), which was moved into take2 (its parameter takes a Buf by value) at line 28, so p can no longer be used here"},
		{"write through", `
func main() {
	b := Buf{n: 1}
	p := &b
	take2(b)
	(*p).n = 2
}`, "p holds a view of b (&b"},
		{"copied pointer", `
func main() {
	b := Buf{n: 1}
	p := &b
	q := p
	take2(b)
	_ = q.n
}`, "q holds a view of b (&b"},
		{"slice of an array", `
func main() {
	var a [2]Buf
	s := a[:]
	a = [2]Buf{}
	_ = len(s)
}`, "s holds a view of a (a[:], line 27), which was given a new value at line 28"},
		{"method value", `
func main() {
	b := Buf{}
	l := b.Len
	b.Consume()
	_ = l()
}`, "l holds a view of b (b.Len, line 27), which was moved by the call b.Consume()"},
		{"maybe moved", `
func main() {
	b := Buf{}
	p := &b
	if cond() {
		take2(b)
	}
	_ = p.n
}`, "p holds a view of b (&b, line 27), which may have been moved into take2"},
		{"field of a struct", `
func main() {
	b := Buf{}
	h := H{p: &b}
	take2(b)
	_ = h.n
	_ = h.p
}`, "h.p holds a view of b (&b, line 27)"},
		{"whole struct", `
func main() {
	b := Buf{}
	var h H
	h.p = &b
	take2(b)
	_ = h
}`, "h holds a view of b (&b, line 28)"},
		{"call result", `
func main() {
	b := Buf{}
	r := first(&b)
	take2(b)
	_ = r
}`, "r holds a view of b (first(&b), line 27)"},
		{"method result", `
func main() {
	b := Buf{}
	r := b.Me()
	b = Buf{}
	_ = r
}`, "r holds a view of b (b.Me(), line 27), which was given a new value at line 28"},
		{"heap memory of an owner", `
func main() {
	b := Buf{}
	s := b.b
	t := b.Bytes()
	b.Consume()
	_ = s
	_ = t
}`, "s holds a view of b (b.b, line 27)"},
		{"function literal", `
func main() {
	b := Buf{}
	p := &b
	f := func() int { return p.n }
	take2(b)
	_ = f()
}`, "b is moved into take2 (its parameter takes a Buf by value) here, but a view of it (&b, line 27) was stored in p at line 27"},
		{"block end", `
func main() {
	var p *Buf
	{
		b := Buf{}
		p = &b
	}
	_ = p
}`, "p holds a view of b (&b, line 29), which was dropped at the end of its block at line 30"},
		{"take", `
func main() {
	b := Buf{}
	p := &b
	c := take(&b)
	_ = c
	_ = p
}`, "p holds a view of b (&b, line 27), which was emptied by take(&b) at line 28"},
		{"stored by a call", `
func main() {
	b := Buf{}
	var h H
	link(&h, &b)
	take2(b)
	_ = h.p
}`, "h.p holds a view of b (&b, line 28)"},
		{"owner holding a view", `
func main() {
	b := Buf{}
	v := V{p: &b}
	take2(b)
	_ = v.p
}`, "v.p holds a view of b (&b, line 27)"},
		{"lent to an interface", `
func main() {
	b := Buf{}
	var x any = b
	take2(b)
	_ = x
}`, "x holds a view of b (b, line 27)"},
		{"range over views", `
func main() {
	b := Buf{}
	ps := [1]*Buf{&b}
	for _, p := range ps {
		_ = p
	}
	take2(b)
	for _, p := range ps {
		_ = p
	}
}`, "ps holds a view of b (&b, line 27)"},
		{"type switch binding", `
func main() {
	b := Buf{}
	var x any = &b
	switch y := x.(type) {
	case *Buf:
		take2(b)
		_ = y
	}
}`, "y holds a view of b (&b, line 27)"},
		{"stored through a pointer", `
func main() {
	var h *Buf
	pp := &h
	b := Buf{}
	*pp = &b
	take2(b)
}`, "b is moved into take2 (its parameter takes a Buf by value) here, but a view of it (&b, line 29) was stored in *pp at line 29, where the compiler cannot check its uses"},
		{"stored by a function literal", `
func main() {
	var h *Buf
	b := Buf{}
	set := func() { h = &b }
	set()
	b.Consume()
	_ = h
}`, "a view of it (&b, line 28) was kept by the function literal at line 28"},
		{"stored by a call through a pointer", `
func main() {
	var h *Buf
	pp := &h
	b := Buf{}
	store(pp, &b)
	b = Buf{}
}`, "b is given a new value here, but a view of it (&b, line 29) was stored by store in what pp points to at line 29"},
		{"stored where a literal reads", `
func main() {
	var h *Buf
	f := func() int { return h.n }
	b := Buf{}
	h = &b
	take2(b)
	_ = f()
}`, "a view of it (&b, line 29) was stored in h at line 29"},
		{"block end of a pinned owner", `
func main() {
	var h *Buf
	pp := &h
	{
		b := Buf{}
		*pp = &b
	}
}`, "b is dropped at the end of its block here"},
	} {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			err := check(t, "package main\n"+viewDecls+c.src+"\n")
			if err == nil {
				t.Fatal("no error reported")
			}
			if msg := fullText(err); !strings.Contains(msg, c.want) {
				t.Errorf("error does not contain %q:\n%s", c.want, msg)
			}
		})
	}
}

func TestViewsAccepted(t *testing.T) {
	testguard.Parallel(t)
	for _, c := range []struct{ name, src string }{
		{"used before the move", `
func main() {
	b := Buf{}
	p := &b
	_ = p.n
	take2(b)
}`},
		{"view made again", `
func main() {
	b := Buf{}
	c := Buf{}
	p := &b
	take2(b)
	p = &c
	_ = p.n
}`},
		{"other fields", `
func main() {
	b := Buf{}
	c := Buf{}
	h := H{p: &b}
	take2(b)
	h.n = 2
	_ = h.n
	h.p = &c
	_ = h.p
}`},
		{"views for a call", `
func main() {
	b := Buf{}
	_ = look(&b)
	_ = b.Len()
	take2(b)
}`},
		{"heap memory kept elsewhere", `
func main() {
	b := Buf{}
	var sink struct{ s []byte }
	ps := &sink
	ps.s = b.Bytes()
	take2(b)
}`},
		{"function literal that gives a new value", `
func main() {
	b := Buf{}
	set := func() { b = Buf{n: 2} }
	take2(b)
	set()
}`},
		{"moved back", `
func main() {
	b := Buf{}
	v := V{p: &b}
	_ = v.p
	w := v
	_ = w.p.n
	take2(b)
}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			if err := check(t, "package main\n"+viewDecls+c.src+"\n"); err != nil {
				t.Errorf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}
