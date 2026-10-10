package escape_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/diag"
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

// fullText returns the diagnostics in err as pluto prints them, with their
// explanations, and with the temporary directory cut from positions.
func fullText(err error) string {
	var b strings.Builder
	diag.Print(&b, err, false)
	return regexp.MustCompile(`/\S*/main\.p`).ReplaceAllString(b.String(), "main.p")
}

// Each rejected program has one escape; want lists substrings of the
// message and its explanation.
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
func main() { _ = f() }`, []string{"main.p:5:9: cannot return &x, a pointer to a local variable\n", "x is a local variable of f (declared at main.p:4)", "return x\n", "x := new(int)\n", "E3001"}},

	{"return composite literal address", `
type T struct{ a int }
func f() *T { return &T{1} }
func main() { _ = f() }`, []string{"cannot return &T{...}, a pointer to a composite literal in f's frame", "return T{...}", "p := new(T)"}},

	{"return interface boxing a struct", `
type E struct{ code, line int }
func (e E) Error() string { return "e" }
func f() error { return E{1, 2} }
func main() { _ = f() }`, []string{"cannot return E{...} as an interface: the interface would hold a copy of it in f's frame", "Converting a value larger than one word"}},

	{"return interface holding pointer to local", `
type E struct{ code int }
func (e *E) Error() string { return "e" }
func f() error {
	var e E
	return &e
}
func main() { _ = f() }`, []string{"cannot return &e, a pointer to a local variable"}},

	{"return capturing closure", `
func f() func() int {
	n := 0
	return func() int { n++; return n }
}
func main() { _ = f() }`, []string{"cannot return the function literal: it uses n, a local variable of f", "type state struct{ n int }"}},

	{"return method value", `
type C struct{ n int }
func (c C) Get() int { return c.n }
func f(c C) func() int { return c.Get }
func main() { _ = f(C{}) }`, []string{"cannot return c.Get, a method value whose receiver is in f's frame"}},

	{"return slice of local array", `
func f() []byte {
	var buf [16]byte
	return buf[:]
}
func main() { _ = f() }`, []string{"cannot return buf[:], a slice of buf, a local array", "return copy(dst, buf[:n])"}},

	{"return slice literal", `
func f() []int { return []int{1, 2, 3} }
func main() { _ = f() }`, []string{"cannot return []int{...}, a slice literal whose elements are in f's frame"}},

	{"named result", `
func f() (p *int) {
	x := 2
	p = &x
	return
}
func main() { _ = f() }`, []string{"main.p:7:1: cannot return p, which points into x, a local variable of f"}},

	{"store in package variable", `
var G *int
func main() {
	x := 1
	G = &x
}`, []string{"cannot store &x in package variable G, which outlives x", "package variables live for the whole program", "G = x\n", "E3002"}},

	{"closure in package variable", `
var F func()
func main() {
	x := 1
	F = func() { x++ }
}`, []string{"cannot store the function literal in package variable F, which outlives x"}},

	{"store in heap object", `
type Node struct{ next *Node }
func main() {
	var x Node
	n := new(Node)
	n.next = &x
}`, []string{"cannot store &x in n.next, which outlives x", "n.next may be on the heap"}},

	{"store through parameter", `
type Node struct{ next *Node }
func link(n *Node) {
	var x Node
	n.next = &x
}
func main() { link(new(Node)) }`, []string{"cannot store &x in n.next, which outlives x", "n.next is reached through n, which points to memory that link's caller owns"}},

	{"callee keeps argument", `
var saved *int
func keep(p *int) { saved = p }
func main() {
	x := 1
	keep(&x)
}`, []string{"main.p:7:2: cannot pass &x to keep, which keeps it after returning", "keep stores p in package variable saved (main.p:4), so &x would outlive x", "E3003"}},

	{"callee keeps argument two calls down", `
var saved *int
func keep(p *int) { saved = p }
func relay(q *int) { keep(q) }
func main() {
	x := 1
	relay(&x)
}`, []string{"cannot pass &x to relay", "relay passes q to keep (main.p:5), which stores p in package variable saved"}},

	{"callee returns its argument", `
func id(p *int) *int { return p }
func f() *int {
	x := 1
	return id(&x)
}
func main() { _ = f() }`, []string{"cannot return id(&x), which points into x, a local variable of f"}},

	{"callee stores argument into heap through another", `
type Node struct{ next *Node }
func link(a, b *Node) { a.next = b }
func main() {
	var x Node
	link(new(Node), &x)
}`, []string{"cannot pass &x to link, which keeps it after returning", "link stores it in a.next"}},

	{"append to heap slice", `
func main() {
	s := make([]*int, 0, 4)
	x := 1
	s = append(s, &x)
}`, []string{"cannot store &x in the backing array of s, which outlives x"}},

	{"variadic array kept", `
var all []*int
func keep(ps ...*int) { all = ps }
func main() {
	x := 1
	keep(&x)
}`, []string{"cannot pass the variadic arguments of keep to keep, which keeps it after returning", "A call with variadic arguments"}},

	{"interface method keeps argument", `
type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	var kp Keeper = k{}
	x := 1
	kp.Keep(&x)
}`, []string{"cannot pass &x to kp.Keep (which may call main.(k).Keep)", "stores p in package variable saved"}},

	{"func value keeps argument", `
var saved *int
func keep(p *int) { saved = p }
func main() {
	f := keep
	x := 1
	f(&x)
}`, []string{"cannot pass &x to f (which may call keep)"}},

	{"closure keeps its argument in a captured variable", `
func each(f func(p *int)) {
	x := 1
	f(&x)
}
func main() {
	var last *int
	each(func(p *int) { last = p })
	_ = last
}`, []string{"cannot pass &x to f (which may call func literal in main", "stores p in last"}},

	{"closure stores its own local outside", `
func main() {
	var p *int
	func() {
		y := 1
		p = &y
	}()
	_ = p
}`, []string{"cannot store &y in p, which outlives y", "The function literal's frame is reclaimed each time"}},

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
}`, []string{"cannot pass &x to f (which may call (k).Keep)"}},

	{"interface method expression keeps argument", `
type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	f := Keeper.Keep
	x := 1
	f(k{}, &x)
}`, []string{"cannot pass &x to f (which may call (k).Keep)"}},

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
}`, []string{"cannot pass &x to w.Keep", "stores p in package variable saved"}},

	{"struct result holding a frame pointer", `
type R struct {
	n int
	p *int
}
func f() R {
	x := 1
	return R{n: 1, p: &x}
}
func main() { _ = f() }`, []string{"cannot return R{...}, which points into x, a local variable of f"}},

	{"external function keeps its arguments by default", `
//pu:extern ext_keep
func extKeep(p *int)
func main() {
	x := 1
	extKeep(&x)
}`, []string{"cannot pass &x to extKeep", "extKeep is implemented outside Plutonium and may keep it"}},

	{"noescape still checks stores between arguments", `
//pu:extern ext_store
//pu:noescape
func extStore(dst **int, p *int)
func main() {
	x := 1
	extStore(new(*int), &x)
}`, []string{"cannot pass &x to extStore", "may store it into memory another argument points to"}},

	{"generic conversion of a struct instantiation", `
type P struct{ x, y int }
func box[T any](x T) any { return x }
func main() { _, _ = box(1), box(P{1, 2}) }`, []string{"cannot return x as an interface: the interface would hold a copy of it in box's frame"}},

	// go statements and channels are rejected by the checker until the
	// scheduler exists; the analysis already treats a go statement's
	// operands and a sent value as escaping.

	{"string over a frame buffer kept", `
import "unsafe"
var name string
func main() {
	var buf [4]byte
	name = unsafe.String(&buf[0], 4)
}`, []string{"cannot store unsafe.String(&buf[0], 4) in package variable name, which outlives buf"}},

	{"type switch extracts frame pointer", `
var G *int
func main() {
	x := 1
	var a any = &x
	switch v := a.(type) {
	case *int:
		G = v
	}
}`, []string{"cannot store v in package variable G, which outlives x", "v points into x."}},

	{"ring push into global ring", `
var r ring[*int, 4]
func main() {
	x := 1
	r.Push(&x)
}`, []string{"cannot store &x in package variable r, which outlives x", "&x is a pointer to x."}},

	// Pointer-shaped structs are the data word of an interface.
	{"pointer-shaped struct holding a frame pointer returned as an error", `
type W struct{ p *int }
func (W) Error() string { return "w" }
func f() error {
	x := 1
	return W{&x}
}
func main() { _ = f() }`, []string{"cannot return W{...}, which points into x, a local variable of f"}},

	// Heap-only fields: soundness.
	{"field given a frame pointer through a parameter", `
type N struct{ next *N }
func link(a, b *N) { a.next = b }
var G *N
func keep(n *N) { G = n.next }
func main() {
	var x, y N
	link(&x, &y)
	keep(&x)
}`, []string{"cannot pass &x to keep, which keeps it after returning"}},

	{"field written through its address", `
type N struct{ p *int }
func set(pp **int, v *int) { *pp = v }
var G *int
func main() {
	var n N
	x := 1
	set(&n.p, &x)
	G = n.p
}`, []string{"cannot store n.p in package variable G, which outlives x"}},

	{"field inside a field written through the outer's address", `
type In struct{ p *int }
type Out struct{ in In }
func set(i *In, v *int) { i.p = v }
var G *int
func main() {
	var o Out
	x := 1
	set(&o.in, &x)
	G = o.in.p
}`, []string{"cannot store o.in.p in package variable G, which outlives x"}},

	// Loops: every iteration reuses the memory of the loop's variables.
	{"closure using the loop variable kept in an outer variable", `
func main() {
	var f0 func() int
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		if i == 0 {
			f0 = f
		}
	}
	println(f0())
}`, []string{"main.p:8:4: cannot keep f in f0 after this iteration: it uses i, which the loop at line 5 reuses in every iteration",
		"i lives in main's frame, where every iteration of the loop at line 5 reuses the same memory for it. When the next iteration starts, the literal kept in f0 would see that iteration's i, not this one's.",
		"Go gives each iteration of a loop its own variables", "var saved int // declared before the loop", "f0 = func(i int) { ... }", "E3006"}},

	{"closure using range variables", `
func main() {
	var g func() int
	for i, v := range [2]int{5, 6} {
		if i == 1 {
			g = func() int { return v + i }
		}
	}
	println(g())
}`, []string{"cannot keep the function literal in g after this iteration: it uses v, which the loop at line 5 reuses in every iteration"}},

	{"pointer to a body variable kept", `
func main() {
	var p *int
	for i := 0; i < 3; i++ {
		x := i
		p = &x
	}
	println(*p)
}`, []string{"cannot keep &x in p after this iteration: the loop at line 5 reuses x in every iteration", "&x is a pointer to x."}},

	{"inner copy of the loop variable kept", `
func main() {
	var f func() int
	for i := 0; i < 3; i++ {
		i := i
		f = func() int { return i }
	}
	println(f())
}`, []string{"cannot keep the function literal in f after this iteration: it uses i, which the loop at line 5 reuses"}},

	{"pointer to a body variable stored in an outer array", `
func main() {
	var ps [3]*int
	for i := 0; i < 3; i++ {
		x := i
		ps[i] = &x
	}
	println(*ps[0])
}`, []string{"cannot keep &x in ps[i] after this iteration: the loop at line 5 reuses x"}},

	{"composite literal kept past the iteration", `
type T struct{ n int }
func main() {
	var p *T
	for i := 0; i < 3; i++ {
		p = &T{i}
	}
	println(p.n)
}`, []string{"cannot keep &T{...} in p after this iteration: the loop at line 6 makes it in the same memory in every iteration"}},

	{"closure stored through a parameter", `
type H struct{ f func() int }
func set(h *H) {
	n := 1
	h.f = func() int { return n }
}
func main() { set(&H{}) }`, []string{"cannot store the function literal in h.f, which outlives n", "reached through h"}},

	{"method value stored in a package variable", `
type C struct{ n int }
func (c C) Get() int { return c.n }
var F func() int
func main() {
	c := C{1}
	F = c.Get
}`, []string{"cannot store c.Get in package variable F, which outlives main's frame", "A method value"}},

	{"interface box stored in a package variable", `
type E struct{ a, b int }
func (E) Error() string { return "e" }
var Err error
func main() {
	Err = E{1, 2}
}`, []string{"cannot store E{...} in package variable Err, which outlives main's frame", "Converting a value larger than one word"}},

	{"composite literal stored in a package variable", `
type T struct{ n int }
var G *T
func main() {
	G = &T{1}
}`, []string{"cannot store &T{...} in package variable G, which outlives main's frame", "A composite literal whose address is taken"}},

	{"resultholds without a parameter of that name", `
type R owner struct{ p *int }
//pu:resultholds q
func hold(p *int) R { return R{p} }
func main() { _ = hold(nil) }`, []string{"//pu:resultholds names q, which is not a parameter of hold", "E3007"}},

	{"resultholds on a function that does not return the parameter", `
var saved *int
type R owner struct{ n int }
//pu:resultholds p
func hold(p *int) R { saved = p; return R{} }
func main() { _ = hold(nil) }`, []string{"//pu:resultholds p, but hold does not return p in its result"}},

	{"result of a resultholds function returned", `
type R owner struct{ p *int }
//pu:resultholds p
func hold(p *int) R {
	saved = p
	return R{p}
}
var saved *int
func f() R {
	x := 1
	return hold(&x)
}
func main() { _ = f() }`, []string{"cannot return hold(&x), which points into x, a local variable of f"}},

	{"loop variable kept in the header of an enclosing loop", `
func main() {
	for p, j := (*int)(nil), 0; j < 2; j++ {
		for i := 0; i < 2; i++ {
			p = &i
		}
		_ = p
	}
}`, []string{"cannot keep &i in p after this iteration: the loop at line 5 reuses i"}},
}

func TestRejected(t *testing.T) {
	for _, c := range rejected {
		t.Run(c.name, func(t *testing.T) {
			err := check(t, "package main\n"+c.src+"\n")
			if err == nil {
				t.Fatal("no escape reported")
			}
			msg := fullText(err)
			// Explanations are wrapped: compare with the line breaks and
			// indentation folded into single spaces.
			flat := strings.Join(strings.Fields(msg), " ")
			for _, w := range c.want {
				if !strings.Contains(flat, strings.Join(strings.Fields(w), " ")) {
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

	{"method value in package initialisation is static", `
type C struct{ n int }
func (c C) Get() int { return c.n }
var c0 = C{3}
var F = c0.Get
func main() { _ = F() }`},

	{"zero-size values box nothing", `
type none struct{}
func (none) Error() string { return "none" }
func f() error { return none{} }
func main() { _ = f() }`},

	{"generic conversion of word-sized instantiations", `
func box[T any](x T) any { return x }
func main() { _, _ = box(1), box(new(int)) }`},

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

	{"resultholds lets a local be held by a local result", `
type R owner struct{ p *int }
var saved *int
//pu:resultholds p
func hold(p *int) R {
	saved = p
	return R{p}
}
func main() {
	x := 1
	r := hold(&x)
	_ = r
}`},
	{"local arena installed as the default allocator", `
import (
	"pluto/alloc"
	"pluto/arena"
)
func main() {
	var mem [256]byte
	a := arena.New(mem[:])
	r := alloc.Use(&a)
	p := new(int)
	*p = 1
	r.Restore()
}`},

	{"pointer-shaped struct holding heap memory returned as an error", `
type W struct{ p *int }
func (W) Error() string { return "w" }
func f() error { return W{new(int)} }
func main() { _ = f() }`},
	{"heap-only fields of a chunk list over a frame-backed parent", `
import "unsafe"
type Allocator interface{ Alloc(n uintptr) unsafe.Pointer }
type chunk struct{ prev *chunk }
type Chunked struct {
	parent      Allocator
	cur, spare  *chunk
}
func (a *Chunked) recycle(c *chunk) { c.prev = a.spare; a.spare = c }
func (a *Chunked) grow() {
	c := a.spare
	if c != nil {
		a.spare = c.prev
	} else {
		c = (*chunk)(a.parent.Alloc(64))
	}
	c.prev = a.cur
	a.cur = c
}
func (a *Chunked) release() {
	c := a.cur
	a.cur = c.prev
	a.recycle(c)
}
func (a *Chunked) Alloc(n uintptr) unsafe.Pointer {
	a.grow()
	return unsafe.Pointer(a.cur)
}
type heap struct{}
func (heap) Alloc(n uintptr) unsafe.Pointer { return unsafe.Pointer(new([64]byte)) }
type counting struct {
	under Allocator
	n     int
}
func (c *counting) Alloc(n uintptr) unsafe.Pointer {
	c.n++
	return c.under.Alloc(n)
}
func main() {
	parent := counting{under: heap{}}
	a := Chunked{parent: &parent}
	_ = a.Alloc(8)
	var inner Chunked
	inner.parent = &a
	_ = inner.Alloc(8)
	a.release()
}`},
	{"generic conversion uses only the instantiations", `
type I interface{ M(p *int) }
type keeper struct{}
var saved *int
func (keeper) M(p *int) { saved = p }
type looker struct{}
func (looker) M(p *int) {}
func box[T any](x T) any { return x }
func call(i I) {
	x := 1
	i.M(&x)
}
func main() {
	_ = box(looker{})
	call(looker{})
	var k keeper
	_ = k
}`},

	// Loops: views of a loop's variables used within the iteration.
	{"closure called within the iteration", `
func main() {
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		println(f())
	}
}`},
	{"closure kept and the loop left", `
func main() {
	var f func() int
	for i := 0; i < 3; i++ {
		if i == 1 {
			f = func() int { return i }
			break
		}
	}
	println(f())
}`},
	{"pointer kept in a body variable", `
func main() {
	for i := 0; i < 3; i++ {
		x := i
		p := &x
		println(*p)
	}
}`},
	{"closure using only outer variables kept", `
func main() {
	n := 5
	var f func() int
	for i := 0; i < 3; i++ {
		f = func() int { return n }
	}
	println(f())
}`},
	{"loop value passed as a parameter", `
func main() {
	var f func(int) int
	for i := 0; i < 3; i++ {
		f = func(i int) int { return i * 10 }
		println(f(i))
	}
}`},
	{"pointer reused across iterations, reset each time", `
func main() {
	var p *int
	for i := 0; i < 3; i++ {
		x := i
		p = &x
		println(*p)
		p = nil
	}
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
