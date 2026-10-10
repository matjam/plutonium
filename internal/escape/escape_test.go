package escape_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// check loads src as a main package and runs the escape analysis on it.
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
	return escape.Check(prog)
}

// Each rejected program has one escape; want lists substrings of the
// message.
var rejected = []struct {
	name string
	src  string
	want []string
}{
	{"return address of local", `
func f() *int {
	x := 1
	return &x
}
func main() { _ = f() }`, []string{"main.p:5:2", "a pointer to local variable x escapes: it is returned from f", "allocate with new"}},

	{"return composite literal address", `
type T struct{ a int }
func f() *T { return &T{1} }
func main() { _ = f() }`, []string{"a pointer to a composite literal", "returned from f"}},

	{"return interface boxing a struct", `
type E struct{ code, line int }
func (e E) Error() string { return "e" }
func f() error { return E{1, 2} }
func main() { _ = f() }`, []string{"an interface value holding a frame copy of a main.E", "returned from f", "section 4.4"}},

	{"return interface holding pointer to local", `
type E struct{ code int }
func (e *E) Error() string { return "e" }
func f() error {
	var e E
	return &e
}
func main() { _ = f() }`, []string{"a pointer to local variable e", "returned from f"}},

	{"return capturing closure", `
func f() func() int {
	n := 0
	return func() int { n++; return n }
}
func main() { _ = f() }`, []string{"a closure capturing n", "returned from f", "section 4.5"}},

	{"return method value", `
type C struct{ n int }
func (c C) Get() int { return c.n }
func f(c C) func() int { return c.Get }
func main() { _ = f(C{}) }`, []string{"a method value bound to c", "returned from f"}},

	{"return slice of local array", `
func f() []byte {
	var buf [16]byte
	return buf[:]
}
func main() { _ = f() }`, []string{"a pointer to local variable buf", "returned from f"}},

	{"return slice literal", `
func f() []int { return []int{1, 2, 3} }
func main() { _ = f() }`, []string{"a slice literal's backing array", "returned from f"}},

	{"named result", `
func f() (p *int) {
	x := 2
	p = &x
	return
}
func main() { _ = f() }`, []string{"a pointer to local variable x", "returned from f"}},

	{"store in package variable", `
var G *int
func main() {
	x := 1
	G = &x
}`, []string{"a pointer to local variable x escapes: it is stored in package variable G"}},

	{"closure in package variable", `
var F func()
func main() {
	x := 1
	F = func() { x++ }
}`, []string{"a closure capturing x", "package variable F"}},

	{"store in heap object", `
type Node struct{ next *Node }
func main() {
	var x Node
	n := new(Node)
	n.next = &x
}`, []string{"a pointer to local variable x escapes: it is stored in n.next, which may be on the heap"}},

	{"store through parameter", `
type Node struct{ next *Node }
func link(n *Node) {
	var x Node
	n.next = &x
}
func main() { link(new(Node)) }`, []string{"a pointer to local variable x", "stored in n.next, which belongs to the caller of link"}},

	{"callee keeps argument", `
var saved *int
func keep(p *int) { saved = p }
func main() {
	x := 1
	keep(&x)
}`, []string{"main.p:7:2", "a pointer to local variable x escapes: it is passed to keep, which stores p in package variable saved (", "main.p:4)"}},

	{"callee keeps argument two calls down", `
var saved *int
func keep(p *int) { saved = p }
func relay(q *int) { keep(q) }
func main() {
	x := 1
	relay(&x)
}`, []string{"passed to relay, which passes q to keep (", "main.p:5), which stores p in package variable saved"}},

	{"callee returns its argument", `
func id(p *int) *int { return p }
func f() *int {
	x := 1
	return id(&x)
}
func main() { _ = f() }`, []string{"a pointer to local variable x", "returned from f"}},

	{"callee stores argument into heap through another", `
type Node struct{ next *Node }
func link(a, b *Node) { a.next = b }
func main() {
	var x Node
	link(new(Node), &x)
}`, []string{"a pointer to local variable x escapes: it is passed to link, which stores it in a.next"}},

	{"append to heap slice", `
func main() {
	s := make([]*int, 0, 4)
	x := 1
	s = append(s, &x)
}`, []string{"a pointer to local variable x", "append writes to"}},

	{"variadic array kept", `
var all []*int
func keep(ps ...*int) { all = ps }
func main() {
	x := 1
	keep(&x)
}`, []string{"the variadic argument array of a call to keep escapes"}},

	{"interface method keeps argument", `
type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	var kp Keeper = k{}
	x := 1
	kp.Keep(&x)
}`, []string{"a pointer to local variable x escapes: it is passed to kp.Keep (which may call main.(k).Keep), which stores p in package variable saved"}},

	{"func value keeps argument", `
var saved *int
func keep(p *int) { saved = p }
func main() {
	f := keep
	x := 1
	f(&x)
}`, []string{"a pointer to local variable x escapes: it is passed to f (which may call keep)"}},

	{"closure keeps its argument in a captured variable", `
func each(f func(p *int)) {
	x := 1
	f(&x)
}
func main() {
	var last *int
	each(func(p *int) { last = p })
	_ = last
}`, []string{"a pointer to local variable x escapes: it is passed to f (which may call func literal in main", "which stores p in last"}},

	{"closure stores its own local outside", `
func main() {
	var p *int
	func() {
		y := 1
		p = &y
	}()
	_ = p
}`, []string{"a pointer to local variable y escapes: it is stored in p, which outlives the function literal"}},

	{"method value in package initialisation", `
type C struct{ n int }
func (c C) Get() int { return c.n }
var c0 = C{3}
var F = c0.Get
func main() { _ = F() }`, []string{"a method value bound to c0", "package variable F"}},

	{"interface method value keeps argument", `
type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	var kp Keeper = k{}
	f := kp.Keep
	x := 1
	f(&x)
}`, []string{"a pointer to local variable x escapes: it is passed to f (which may call (k).Keep)"}},

	{"interface method expression keeps argument", `
type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	f := Keeper.Keep
	x := 1
	f(k{}, &x)
}`, []string{"a pointer to local variable x escapes: it is passed to f (which may call (k).Keep)"}},

	{"embedded interface keeps argument", `
type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
type wrap struct{ Keeper }
func main() {
	w := wrap{k{}}
	x := 1
	w.Keep(&x)
}`, []string{"a pointer to local variable x", "stores p in package variable saved"}},

	{"struct result holding a frame pointer", `
type R struct {
	n int
	p *int
}
func f() R {
	x := 1
	return R{n: 1, p: &x}
}
func main() { _ = f() }`, []string{"a pointer to local variable x", "returned from f"}},

	{"external function keeps its arguments by default", `
//pu:extern ext_keep
func extKeep(p *int)
func main() {
	x := 1
	extKeep(&x)
}`, []string{"passed to extKeep, which is implemented outside Plutonium and may keep it"}},

	{"noescape still checks stores between arguments", `
//pu:extern ext_store
//pu:noescape
func extStore(dst **int, p *int)
func main() {
	x := 1
	extStore(new(*int), &x)
}`, []string{"a pointer to local variable x", "may store it into memory another argument points to"}},

	// go statements and channels are rejected by the checker until the
	// scheduler exists; the analysis already treats a go statement's
	// operands and a sent value as escaping.

	{"string over a frame buffer kept", `
import "unsafe"
var name string
func main() {
	var buf [4]byte
	name = unsafe.String(&buf[0], 4)
}`, []string{"a pointer to local variable buf", "package variable name"}},

	{"type switch extracts frame pointer", `
var G *int
func main() {
	x := 1
	var a any = &x
	switch v := a.(type) {
	case *int:
		G = v
	}
}`, []string{"a pointer to local variable x", "package variable G"}},

	{"ring push into global ring", `
var r ring[*int, 4]
func main() {
	x := 1
	r.Push(&x)
}`, []string{"a pointer to local variable x", "stored in r"}},
}

func TestRejected(t *testing.T) {
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			err := check(t, "package main\n"+c.src+"\n")
			if err == nil {
				t.Fatal("no escape reported")
			}
			msg := err.Error()
			for _, w := range c.want {
				if !strings.Contains(msg, w) {
					t.Errorf("error does not contain %q:\n%s", w, msg)
				}
			}
		})
	}
}

var accepted = []struct{ name, src string }{
	{"pass address down", `
func set(p *int) { *p = 2 }
func main() {
	x := 1
	set(&x)
	_ = x
}`},

	{"linked list on the stack", `
type Node struct {
	next *Node
	v    int
}
func sum(n *Node) int {
	s := 0
	for ; n != nil; n = n.next {
		s += n.v
	}
	return s
}
func main() {
	var a, b, c Node
	a.next = &b
	b.next = &c
	_ = sum(&a)
}`},

	{"initialise a local through a method", `
type Scanner struct {
	src []byte
	pos int
}
func (s *Scanner) Init(src []byte) { s.src = src; s.pos = 0 }
func main() {
	var buf [8]byte
	var s Scanner
	s.Init(buf[:])
}`},

	{"return heap pointer", `
type T struct{ a int }
func f() *T {
	p := new(T)
	p.a = 1
	return p
}
func main() { _ = f() }`},

	{"return parameter", `
func id(p *int) *int { return p }
func main() {
	x := 1
	q := id(&x)
	*q = 2
}`},

	{"return word-sized error", `
type Errno int
func (e Errno) Error() string { return "errno" }
func f() error { return Errno(2) }
func main() { _ = f() }`},

	{"return address of package variable", `
type E struct{ code int }
func (e *E) Error() string { return "e" }
var errBad = E{1}
func f() error { return &errBad }
func main() { _ = f() }`},

	{"package initialisation literals are static", `
type T struct{ a int }
type S struct{ s string; n int }
var P = &T{1}
var L = []int{1, 2, 3}
var A any = S{"x", 2}
func main() { _, _, _ = P, L, A }`},

	{"closure called locally", `
func apply(f func()) { f() }
func main() {
	n := 0
	apply(func() { n++ })
	_ = n
}`},

	{"printf with frame boxes", `
import "pluto/fmt"
type P struct{ x, y int }
func main() {
	s := "hello"
	fmt.Printf("%s %v %d\n", s, P{1, 2}, 3)
}`},

	{"variable reused for frame and heap values", `
type E struct{ code int }
func (e *E) Error() string { return "e" }
var saved error
func main() {
	var local E
	var err error = &local
	_ = err.Error()
	err = new(E)
	saved = err
}`},

	{"branches join", `
var G *int
func main() {
	x := 1
	p := &x
	if x > 0 {
		p = new(int)
		G = p
	}
	_ = p
}`},

	{"store through pointer to local struct", `
type Box struct{ p *int }
func main() {
	x := 1
	var b Box
	q := &b
	q.p = &x
}`},

	{"vec of ints on a local allocator value", `
import (
	"pluto/alloc"
	"pluto/vec"
)
func main() {
	v := vec.New[int](alloc.Default())
	v.Push(1)
	v.Free()
}`},

	{"noescape external function", `
//pu:extern ext_read
//pu:noescape
func extRead(p *int) int
func main() {
	x := 1
	_ = extRead(&x)
}`},

	{"loop reassigns a tracked variable", `
type Node struct{ next *Node }
var G *Node
func main() {
	var local Node
	p := &local
	for i := 0; i < 3; i++ {
		_ = p.next
		p = new(Node)
	}
	q := new(Node)
	for i := 0; i < 2; i++ {
		if i == 1 {
			break
		}
		q = new(Node)
	}
	G = q
}`},

	{"switch clauses join", `
var G error
type E int
func (e E) Error() string { return "e" }
func pick(n int) error {
	var err error
	switch n {
	case 0:
		err = E(1)
	case 1:
		err = E(2)
		fallthrough
	default:
		err = E(3)
	}
	return err
}
func main() { G = pick(1) }`},

	{"struct fields told apart", `
type Pair struct {
	a *int
	b *int
}
var G *int
func main() {
	x := 1
	p := Pair{a: &x, b: new(int)}
	G = p.b
}`},
}

func TestAccepted(t *testing.T) {
	for _, c := range accepted {
		t.Run(c.name, func(t *testing.T) {
			if err := check(t, "package main\n"+c.src+"\n"); err != nil {
				t.Fatalf("unexpected escape: %v", err)
			}
		})
	}
}
