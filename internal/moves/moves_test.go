package moves_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/moves"
	"github.com/matjam/plutonium/internal/types2"
)

// check loads src as a main package and runs the move checker on it.
func check(t *testing.T, src string) error {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return moves.Check(prog)
}

// fullText returns the diagnostics in err as pluto prints them, with the
// temporary directory cut from positions and the lines folded.
func fullText(err error) string {
	var b strings.Builder
	diag.Print(&b, err, false)
	s := regexp.MustCompile(`/\S*/main\.p`).ReplaceAllString(b.String(), "main.p")
	return strings.Join(strings.Fields(s), " ")
}

const decls = `
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
`

// Each rejected program has errors; want lists substrings of the output.
var rejected = []struct {
	name string
	src  string
	want []string
}{
	{"use after assignment", `
func main() {
	m := Buf{n: 1}
	n := m
	_ = look(&m)
	_ = n
}`, []string{"main.p:27:12: m was moved to n at line 26, so it can no longer be used here",
		"Buf is an owner type: its values own what they hold and release it when they are dropped. Assigning an owner moves it instead of copying it",
		"Since line 26, n owns it and m holds nothing.",
		"n := m.Clone()", "n := &m", "E3301"}},

	{"use after passing", `
func main() {
	m := Buf{}
	_ = take2(m)
	_ = m.Len()
}`, []string{"m was moved into take2 (its parameter takes a Buf by value) at line 26", "take2(&m)"}},

	{"use after value receiver", `
func main() {
	m := Buf{}
	_ = m.Consume()
	_ = m.Len()
}`, []string{"m was moved by the call m.Consume() (Consume has a value receiver) at line 26", "func (x *Buf) Consume(...)"}},

	{"use after drop", `
func main() {
	f := File{}
	f.Drop()
	_ = f
}`, []string{"f was dropped by f.Drop() at line 26", "Calling Drop ends an owner's life"}},

	{"maybe moved", `
func main() {
	b := Buf{}
	if cond() {
		_ = take2(b)
	}
	_ = b.Len()
}`, []string{"main.p:29:6: b may have been moved at line 27, so it cannot be used here", "E3302"}},

	{"moved in the previous iteration", `
func main() {
	b := Buf{}
	for i := 0; i < 2; i++ {
		_ = take2(b)
	}
}`, []string{"b was moved into take2 (its parameter takes a Buf by value) at line 27 in the previous iteration of this loop", "Declare b inside the loop"}},

	{"move out of a field", `
func main() {
	c := Conn{}
	b := c.buf
	_ = b
}`, []string{"cannot move c.buf out of c: c.buf is a field, not a variable", "v := take(&c.buf)", "v := &c.buf", "E3303"}},

	{"move out of an element", `
func main() {
	var a [2]Buf
	b := a[0]
	_ = b
}`, []string{"cannot move a[0] out of a: a[0] is an element, not a variable"}},

	{"move through a pointer", `
func main() {
	b := Buf{}
	p := &b
	c := *p
	_ = c
}`, []string{"cannot move *p out of the value p points to", "v := take(p)"}},

	{"move a package variable", `
func main() {
	b := global
	_ = b
}`, []string{"cannot move package variable global"}},

	{"range copies owners", `
func main() {
	var a [2]Buf
	for _, b := range a {
		_ = b.Len()
	}
}`, []string{"cannot copy the elements of a in a range loop: Buf is an owner type", "for i := range a {", "E3304"}},

	{"move inside a literal", `
func main() {
	b := Buf{}
	f := func() { _ = take2(b) }
	f()
}`, []string{"cannot move b inside a function literal: the literal may run more than once, or never", "do := func(b Buf) { ... }", "E3305"}},

	{"copy owners", `
func main() {
	var a, c [2]Buf
	copy(a[:], c[:])
}`, []string{"cannot copy elements of type Buf: each element is an owner", "E3306"}},

	{"drop through a pointer", `
func main() {
	f := File{}
	p := &f
	p.Drop()
}`, []string{"cannot call p.Drop(): p is a *File, which only points at a File that something else owns", "E3307"}},

	{"assert an owner out of any", `
func main() {
	var x any
	b := x.(Buf)
	_ = b
}`, []string{"cannot assert x to Buf: x is an any, which only lends the value it holds", "E3308"}},

	{"owners as variadic arguments", `
func all(bs ...Buf) {}
func main() {
	b := Buf{}
	all(b)
}`, []string{"cannot pass b to all as a variadic argument: Buf is an owner type", "E3310"}},

	{"deferred call uses an inner owner", `
func main() {
	if cond() {
		f := File{}
		defer look2(&f)
	}
}
func look2(f *File) {}`, []string{"the deferred call uses f, which is dropped at the end of its block", "E3311"}},

	{"deferred literal uses an owner moved later", `
func main() {
	b := Buf{}
	defer func() { _ = look(&b) }()
	_ = take2(b)
}`, []string{"b was moved into take2 (its parameter takes a Buf by value) at line 27, but the deferred function (line 26) uses it when the function returns", "E3301"}},
	{"deferred literal uses an owner moved on some path", `
func main() {
	b := Buf{}
	defer func() { _ = look(&b) }()
	if cond() {
		_ = take2(b)
	}
}`, []string{"b may have been moved into take2 (its parameter takes a Buf by value) at line 28, but the deferred function (line 26) uses it when the function returns", "E3302"}},
	{"deferred literal uses an owner moved in a branch that returns", `
func f() int {
	b := Buf{}
	defer func() { _ = look(&b) }()
	if cond() {
		return take2(b)
	}
	return 0
}
func main() { _ = f() }`, []string{"b was moved into take2 (its parameter takes a Buf by value) at line 28, but the deferred function (line 26) uses it when the function returns", "E3301"}},
	{"deferred literal uses a dropped owner", `
func main() {
	f := File{}
	defer func() { _ = f }()
	f.Drop()
}`, []string{"f was dropped by f.Drop() at line", "but the deferred function (line", "E3301"}},

	{"deferred call keeps the address of an owner moved later", `
func main() {
	b := Buf{}
	defer look(&b)
	_ = take2(b)
}`, []string{"b was moved into take2 (its parameter takes a Buf by value) at line 27, but the deferred call (line 26) uses it when the function returns", "keeps a view of b", "E3301"}},
	{"deferred call keeps the address of an owner moved on some path", `
func main() {
	b := Buf{}
	defer look(&b)
	if cond() {
		_ = take2(b)
	}
}`, []string{"b may have been moved into take2 (its parameter takes a Buf by value) at line 28, but the deferred call (line 26) uses it when the function returns", "E3302"}},
	{"deferred call keeps a slice view of an owner dropped later", `
func main() {
	b := Buf{}
	defer sink(b.b[1:])
	_ = take2(b)
}
func sink(p []byte) {}`, []string{"b was moved into take2 (its parameter takes a Buf by value) at line 27, but the deferred call (line 26)", "E3301"}},
	{"deferred call keeps a field view of an owner moved later", `
func main() {
	b := Buf{}
	defer sink(b.b)
	_ = take2(b)
}
func sink(p []byte) {}`, []string{"but the deferred call (line 26) uses it when the function returns", "E3301"}},
	{"deferred call keeps a view held in another variable", `
func main() {
	b := Buf{}
	p := &b
	defer look(p)
	_ = take2(b)
}`, []string{"but the deferred call (line 27) uses it when the function returns", "E3301"}},
	{"deferred method call takes the address of the receiver", `
func main() {
	b := Buf{}
	defer b.Len()
	_ = take2(b)
}`, []string{"but the deferred call (line 26) uses it when the function returns", "E3301"}},
	{"deferred call in a branch that moves after it", `
func main() {
	b := Buf{}
	if cond() {
		defer look(&b)
	}
	_ = take2(b)
}`, []string{"b was moved into take2", "but the deferred call (line 27)", "E3301"}},
	{"stored literal deferred, owner moved later", `
func main() {
	b := Buf{}
	f := func() { _ = look(&b) }
	defer f()
	_ = take2(b)
}`, []string{"b was moved into take2 (its parameter takes a Buf by value) at line 28, but the deferred function (line 27) uses it when the function returns", "The function f, which holds a function literal that uses b", "E3301"}},
	{"stored literal assigned later and deferred", `
func main() {
	b := Buf{}
	var f func()
	f = func() { _ = look(&b) }
	defer f()
	_ = take2(b)
}`, []string{"but the deferred function (line 28) uses it when the function returns", "E3301"}},
	{"stored literal deferred through an alias", `
func main() {
	b := Buf{}
	f := func() { _ = look(&b) }
	g := f
	defer g()
	_ = take2(b)
}`, []string{"The function g, which holds a function literal that uses b", "E3301"}},
	{"literal passed to a deferred call", `
func main() {
	b := Buf{}
	defer run(func() { _ = look(&b) })
	_ = take2(b)
}
func run(f func()) { f() }`, []string{"but the deferred function (line 26) uses it when the function returns", "E3301"}},

	{"slice literal of owners", `
func main() {
	b := Buf{}
	s := []Buf{b}
	_ = s
}`, []string{"a slice literal cannot hold owners: Buf is an owner type", "E3312"}},

	{"index a container of owners", `
type Bufs owner struct{ data [4]Buf }
func (v *Bufs) Index(i int) Buf { return take(&v.data[i]) }
func main() {
	var v Bufs
	b := v[0]
	_ = b
}`, []string{"cannot read v[0]: the elements of v are owners, and v[0] would be a copy", "p := v.Ptr(0)", "E3309"}},

	{"named result moved before a bare return", `
func f() (b Buf) {
	_ = take2(b)
	return
}
func main() { _ = f() }`, []string{"b was moved into take2"}},

	{"owner moved into a promoted field of a literal", `
type Wrap owner struct {
	Conn
	n int
}
func main() {
	b := Buf{n: 1}
	w := Wrap{buf: b, n: 2}
	_ = w
	_ = look(&b)
}`, []string{"b was moved", "E3301"}},
}

func TestRejected(t *testing.T) {
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			err := check(t, "package main\n"+decls+c.src+"\n")
			if err == nil {
				t.Fatal("no error reported")
			}
			msg := fullText(err)
			for _, w := range c.want {
				if !strings.Contains(msg, strings.Join(strings.Fields(w), " ")) {
					t.Errorf("error does not contain %q:\n%s", w, msg)
				}
			}
		})
	}
}

var accepted = []struct{ name, src string }{
	{"moves and new values", `
func main() {
	m := Buf{}
	n := m
	m = Buf{}
	_ = m.Len() + n.Len()
}`},
	{"pointer receivers borrow", `
func main() {
	m := Buf{}
	_ = m.Len()
	_ = look(&m)
	_ = take2(m)
}`},
	{"move on both branches, then reassign", `
func main() {
	b := Buf{}
	if cond() {
		_ = take2(b)
	} else {
		_ = b.Consume()
	}
	b = Buf{}
	_ = b.Len()
}`},
	{"loop that gives the variable a new value", `
func main() {
	b := Buf{}
	for i := 0; i < 3; i++ {
		_ = take2(b)
		b = Buf{}
	}
	_ = b.Len()
}`},
	{"declared in the loop", `
func main() {
	for i := 0; i < 3; i++ {
		b := Buf{}
		_ = take2(b)
	}
}`},
	{"take and permutation", `
func main() {
	c := Conn{}
	b := take(&c.buf)
	var a [2]Buf
	a[0], a[1] = a[1], a[0]
	_ = b.Len()
}`},
	{"stored into fields and literals", `
func main() {
	b, d := Buf{}, Buf{}
	c := Conn{buf: b}
	c.buf = d
	_ = c
}`},
	{"returned", `
func mk() Buf {
	b := Buf{}
	return b
}
func main() { _ = mk() }`},
	{"lent to an interface", `
func main() {
	b := Buf{}
	var x any = &b
	_ = x
	println(b.Len())
}`},
	{"literal uses a captured owner", `
func main() {
	b := Buf{}
	f := func() int { return b.Len() }
	_ = f()
	_ = take2(b)
}`},
	{"deferred literal, owner never moved", `
func main() {
	b := Buf{}
	defer func() { _ = look(&b) }()
	_ = b.Len()
}`},
	{"deferred literal, owner moved before the defer statement and given a value", `
func main() {
	b := Buf{}
	_ = take2(b)
	b = Buf{}
	defer func() { _ = look(&b) }()
}`},
	{"deferred literal, owner moved in a branch that returns before the defer", `
func f() int {
	b := Buf{}
	if cond() {
		return take2(b)
	}
	defer func() { _ = look(&b) }()
	return 0
}
func main() { _ = f() }`},
	{"deferred literal, owner moved and given a value again", `
func main() {
	b := Buf{}
	defer func() { _ = look(&b) }()
	_ = take2(b)
	b = Buf{}
}`},
	{"deferred literal, another owner moved", `
func main() {
	b, c := Buf{}, Buf{}
	defer func() { _ = look(&b) }()
	_ = take2(c)
}`},
	{"deferred call, argument is a copy of a non-owner field", `
func main() {
	b := Buf{}
	defer num(b.n)
	_ = take2(b)
}
func num(n int) {}`},
	{"deferred call keeps the address, owner never moved", `
func main() {
	b := Buf{}
	defer look(&b)
	_ = b.Len()
}`},
	{"deferred call keeps the address, owner moved and given a value again", `
func main() {
	b := Buf{}
	defer look(&b)
	_ = take2(b)
	b = Buf{}
}`},
	{"deferred call, owner moved before the defer and given a value", `
func main() {
	b := Buf{}
	_ = take2(b)
	b = Buf{}
	defer look(&b)
}`},
	{"deferred call, owner moved in a branch that returns before the defer", `
func f() int {
	b := Buf{}
	if cond() {
		return take2(b)
	}
	defer look(&b)
	return 0
}
func main() { _ = f() }`},
	{"deferred call keeps the address of another owner", `
func main() {
	b, c := Buf{}, Buf{}
	defer look(&b)
	_ = take2(c)
}`},
	{"stored literal deferred, owner never moved", `
func main() {
	b := Buf{}
	f := func() { _ = look(&b) }
	defer f()
	_ = b.Len()
}`},
	{"stored literal deferred, owner moved before the literal is made", `
func main() {
	b := Buf{}
	_ = take2(b)
	b = Buf{}
	f := func() { _ = look(&b) }
	defer f()
}`},
	{"stored literal deferred, it uses no owner", `
func main() {
	b := Buf{}
	n := 3
	f := func() { _ = n }
	defer f()
	_ = take2(b)
}`},
	{"stored literal called, not deferred", `
func main() {
	b := Buf{}
	f := func() { _ = look(&b) }
	f()
	_ = take2(b)
	b = Buf{}
}`},

	{"drop on the variable", `
func main() {
	f := File{}
	f.Drop()
	f = File{}
	_ = f
}`},
	{"break after a move", `
func main() {
	b := Buf{}
	for {
		_ = take2(b)
		break
	}
}`},
	{"goto back to a new value", `
func main() {
	n := 0
L:
	b := Buf{}
	_ = take2(b)
	n++
	if n < 3 {
		goto L
	}
}`},
}

func TestAccepted(t *testing.T) {
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			if err := check(t, "package main\n"+decls+c.src+"\n"); err != nil {
				t.Fatalf("unexpected error:\n%s", fullText(err))
			}
		})
	}
}
