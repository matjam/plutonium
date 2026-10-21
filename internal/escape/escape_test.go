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
	if strings.Contains(src, "//pu:resultholds") && !strings.Contains(src, "// a user package") {
		// Tests of the directive itself, which only the standard
		// library may use.
		return escape.CheckUserResultHolds(prog)
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
	{"type assertion and a method that keeps its receiver, on a frame pointer", `
type Stringer interface{ String() string }
type Named struct{ name string }
var last *Named
func (n *Named) String() string {
	last = n
	return "named"
}
type Wide struct{ a, b, c int }
func (w Wide) String() string { return "wide" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func main() {
	var n Named
	println(call(&n), call(Wide{1, 2, 3}))
}`, []string{"cannot pass &n to call", "package variable last"}},
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

	{"closure from a call stored in a package variable", `
var saved func() int
func f() func() int {
	n := 0
	return func() int { n++; return n }
}
func main() { saved = f() }`, []string{"cannot store f() in package variable saved", "f() returns a closure, which it builds in a region of its caller's frame"}},

	{"method value from a call stored in a package variable", `
type C struct{ n int }
func (c C) Get() int { return c.n }
func f(c C) func() int { return c.Get }
var saved func() int
func main() { saved = f(C{}) }`, []string{"cannot store f(C{}) in package variable saved"}},

	{"closure from a call's region returned by calling its closure", `
var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func own(n int) func() func() int {
	inner := inc(n)
	return func() func() int { return inner }
}
func h() { saved = own(3)() }
func main() { h() }`, []string{"cannot store own(3)() in package variable saved", "own(3) returns a closure, which it builds in a region of its caller's frame"}},

	{"closure from a call's region returned through a parameter", `
var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func own(n int) func() func() int {
	inner := inc(n)
	return func() func() int { return inner }
}
func get(f func() func() int) func() int { return f() }
func h() { saved = get(own(3)) }
func main() { h() }`, []string{"cannot store get(own(3)) in package variable saved"}},

	{"closure from a call's region yielded", `
var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func gen(n int) func(func(func() int) bool) {
	inner := inc(n)
	return func(yield func(func() int) bool) { yield(inner) }
}
func h() {
	for f := range gen(3) {
		saved = f
	}
}
func main() { h() }`, []string{"cannot pass inner to yield (which may call the loop body at main.p:10)", "stores f in package variable saved (main.p:11)"}},

	{"address of a variable in a call's region returned by calling its closure", `
var saved *int
func cell(n int) func() *int { return func() *int { return &n } }
func h() { saved = cell(3)() }
func main() { h() }`, []string{"cannot store cell(3)() in package variable saved"}},

	{"pointer to a variable in a call's region returned by calling its closure", `
var saved *int
func mk() func() *int {
	x := 3
	p := &x
	return func() *int {
		x++
		return p
	}
}
func h() { saved = mk()() }
func main() { h() }`, []string{"cannot store mk()() in package variable saved"}},

	{"parameter set to a closure built in the region, returned by calling the closure", `
var saved func() int
func inc(n int) func() int { return func() int { n++; return n } }
func swap(f func() int) func() func() int {
	f = inc(3)
	return func() func() int { return f }
}
func h() { saved = swap(nil)() }
func main() { h() }`, []string{"cannot store swap(nil)() in package variable saved"}},

	{"closure returned in the caller's region pointing into the frame", `
func mk() func() int {
	x := 3
	p := &x
	return func() int { return *p }
}
func main() { println(mk()()) }`, []string{"cannot return the function literal, which points into x, a local variable of mk"}},

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
import "pluto/alloc"
type Node struct{ next *Node }
func link(n *Node) {
	var x Node
	n.next = &x
}
func main() { link(alloc.New[Node]()) }`, []string{"cannot store &x in n.next, which outlives x", "n.next is reached through n, which points to memory that link's caller owns"}},

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
import "pluto/alloc"
type Node struct{ next *Node }
func link(a, b *Node) { a.next = b }
func main() {
	var x Node
	link(alloc.New[Node](), &x)
}`, []string{"cannot pass &x to link, which keeps it after returning", "link stores it in a.next"}},

	{"copy to heap slice", `
func main() {
	box := new([4]*int)
	s := box[:]
	x := 1
	src := [1]*int{&x}
	copy(s, src[:])
}`, []string{"cannot store", "backing array of s"}},

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
import "pluto/alloc"
//pu:extern ext_store
//pu:noescape
func extStore(dst **int, p *int)
func main() {
	x := 1
	extStore(alloc.New[*int](), &x)
}`, []string{"cannot pass &x to extStore", "may store it into memory another argument points to"}},

	{"generic conversion of a struct instantiation", `
type P struct{ x, y int }
func box[T any](x T) any { return x }
func main() { _, _ = box(1), box(P{1, 2}) }`, []string{"cannot return x as an interface: the interface would hold a copy of it in box's frame"}},

	// task statements and channels are rejected by the checker until the
	// scheduler exists; the analysis already treats a task statement's
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

	// Loops: a called function keeps the view, so the message names the
	// callee, not a destination.
	{"pointer to a body variable kept by a callee", `
func put(buf []*int, i int, p *int) { buf[i] = p }
func main() {
	var arr [4]*int
	buf := arr[:]
	for i := 0; i < 3; i++ {
		v := i
		put(buf, i, &v)
	}
	println(len(arr))
}`, []string{"main.p:9:3: cannot pass &v to put, which keeps it after this iteration: the loop at line 7 reuses v in every iteration\n",
		"&v is a pointer to v. v lives in main's frame, where every iteration of the loop at line 7 reuses the same memory for it. put stores it in buf[i] (main.p:3). When the next iteration starts, what put kept would see that iteration's v, not this one's.",
		"If put needs the view only during the call, change it so it does not keep it. To use the value later, keep the value instead of a view of it:", "saved = v", "E3006"}},

	{"closure using the loop variable kept by a callee", `
func put(fs []func() int, i int, f func() int) { fs[i] = f }
func main() {
	var fs [3]func() int
	for i := 0; i < 3; i++ {
		f := func() int { return i * 10 }
		put(fs[:], i, f)
	}
	println(fs[0]())
}`, []string{"main.p:8:3: cannot pass f to put, which keeps it after this iteration: it uses i, which the loop at line 6 reuses in every iteration\n",
		"i lives in main's frame, where every iteration of the loop at line 6 reuses the same memory for it. put stores it in fs[i] (main.p:3). When the next iteration starts, the literal kept by put would see that iteration's i, not this one's.",
		"If put needs the function literal only during the call, change it so it does not keep it. To use i later, keep its value instead of the function literal:",
		"Or have the function literal take i as a parameter, and pass it where the literal is called.", "E3006"}},

	{"composite literal kept by a callee", `
type T struct{ n int }
func put(ps []*T, i int, p *T) { ps[i] = p }
func main() {
	var ps [3]*T
	for i := 0; i < 3; i++ {
		put(ps[:], i, &T{i})
	}
	println(ps[0].n)
}`, []string{"main.p:8:3: cannot pass &T{...} to put, which keeps it after this iteration: the loop at line 7 makes it in the same memory in every iteration\n",
		"Every iteration of the loop at line 7 reuses that memory. put stores it in ps[i] (main.p:4). When the next iteration starts, what put kept would see that iteration's value, not this one's.",
		"Change put so it does not keep the view, or pass a copy of the value instead of a view of it."}},

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

	{"resultholds in a user package", `
// a user package
type R owner struct{ p *int }
//pu:resultholds p
func hold(p *int) R { return R{p} }
func main() { _ = hold(nil) }`, []string{"main.p:6:1: //pu:resultholds is for the standard library; hold cannot use it", "E3008"}},

	{"returning the Restore of a local arena", `
import (
	"pluto/alloc"
	"pluto/arena"
)
func install() alloc.Restore {
	a := arena.NewFixed(64)
	return alloc.Use(&a)
}
func main() { install().Restore() }`, []string{"cannot return alloc.Use(&a)"}},

	{"address of a variable a returned closure captures", `
func f(n int) (func() int, *int) {
	return func() int { return n }, &n
}
func main() {
	g, p := f(1)
	*p = 2
	println(g())
}`, []string{"cannot return &n"}},

	{"task statement whose function keeps its packed variadic arguments", `
var kept []int
func keep(xs ...int) { kept = xs }
func main() { task keep(1, 2) }`, []string{"cannot pass the variadic arguments of keep to a task statement"}},

	{"constructor yielding the address of what it captures", `
var saved *int
func ptrs(n int) func(func(*int) bool) {
	return func(yield func(*int) bool) { yield(&n) }
}
func main() {
	for p := range ptrs(1) {
		saved = p
	}
}`, []string{"cannot pass &n to yield (which may call the loop body at main.p:8)", "stores p in package variable saved (main.p:9)"}},

	{"field of a box returned", `
type N struct{ v int }
func f() *int {
	n := new(N)
	return &n.v
}
func main() { _ = f() }`, []string{"cannot return &n.v, which points into the object n refers to"}},

	{"box view stored globally", `
type N struct{ v int }
var G *N
func f() {
	n := new(N)
	G = (*N)(n)
}
func main() { f() }`, []string{"cannot store (*N)(n) in package variable G, which outlives n"}},

	{"temporary box viewed", `
type N struct{ v int }
func f() *N { return (*N)(new(N)) }
func main() { _ = f() }`, []string{"the temporary owned[N]", "E3009"}},

	{"temporary channel lent to an ordinary interface", `
func f() any { return make(chan int) }
func main() { _ = f() }`, []string{"cannot return make(chan int) as an interface: an ordinary interface only borrows the channel",
		"Return the channel itself, changing the result type to chan int"}},

	{"channel parameter lent to an ordinary interface", `
var kept any
func f(c chan int) { kept = c }
func main() { f(nil) }`, []string{"cannot store c in package variable kept, which outlives c", "a counted reference to a channel",
		"Store c itself, changing the type of kept to chan int"}},

	{"box lent to an ordinary interface", `
type N struct{ v int }
func (n *N) Get() int { return n.v }
type G interface{ Get() int }
func f() G {
	n := new(N)
	return n
}
func main() { _ = f() }`, []string{"cannot return n as an interface: an ordinary interface only borrows the box"}},

	{"string from a deferred local arena", `
import (
	"pluto/alloc"
	"pluto/arena"
)
func f(a, b string) string {
	ar := arena.NewFixed(256)
	defer alloc.Use(&ar).Restore()
	return a + b
}
func main() { _ = f("x", "y") }`, []string{"memory allocated from ar, a local arena of f"}},

	{"arena string converted to an interface in a window", `
import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)
func f(a, b string) {
	ar := arena.NewFixed(256)
	defer alloc.Use(&ar).Restore()
	s := a + b
	fmt.Println(s)
}
func main() { f("x", "y") }`, []string{"cannot convert s to an interface", "memory allocated from ar, a local arena of f", "after the arena ar is dropped"}},

	// Found by the lowering oracle (tools/escapeoracle): pluto/fmt formats
	// a struct it reaches inside a box by making a *T to it (rt.Make with
	// PtrTo) and calling a Lister's FmtList, a method of *T that no
	// conversion in the program lists. The interior pointer into the box
	// must not be dropped by the pointer filter either.
	{"fmt calls a pointer method of a struct field inside a box", `
import "pluto/fmt"
var kept *S
type S struct{ n int }
func (s *S) FmtList() (any, int) {
	kept = s
	return nil, 0
}
type Outer struct {
	a int
	s S
}
func f() {
	o := Outer{a: 1, s: S{n: 7}}
	fmt.Println(o)
}
func main() { f() }`, []string{"cannot pass o to fmt.Println, which keeps it", "may call main.(*S).FmtList", "stores s in package variable kept"}},

	// Found by the lowering oracle: a default parameter value is evaluated
	// at the call, its temporaries in the caller's frame (section 10.13).
	{"default value with a slice literal kept by the callee", `
var kept []int
func seven() int { return 7 }
func keep(s []int = []int{seven(), seven()}) { kept = s }
func f() { keep() }
func main() { f() }`, []string{"cannot pass []int{...} to keep, which keeps it", "slice literal (at main.p:5) keeps its elements in an array in f's frame"}},

	// Review 5 (a1): a ring's iterator is a closure record holding the
	// ring's address.
	{"iterator of a local ring stored in a package variable", `
import "pluto/iter"
var g iter.Seq2[int, *int]
func f() {
	var r ring[*int, 4]
	g = r.All()
}
func main() { f() }`, []string{"cannot store r.All() in package variable g", "r.All() points into r"}},

	// A ring of owners yields views of its elements (section 7.17).
	{"view of a local ring's owner element kept", `
type Conn owner struct{ id int }
func (c *Conn) Drop() {}
var kept *Conn
func f() {
	var r ring[Conn, 4]
	r.Push(Conn{id: 1})
	if c, ok := r.Peek(); ok {
		kept = c
	}
}
func main() { f() }`, []string{"cannot store c in package variable kept", "points into r"}},

	{"drop of a local ring's elements keeps them", `
type Conn owner struct{ id int }
var last *Conn
func (c *Conn) Drop() { last = c }
func f() {
	var r ring[Conn, 4]
	r.Push(Conn{id: 1})
	r.Reset()
}
func main() { f() }`, []string{"which keeps it after returning", "stores c in package variable last"}},

	{"range over a local ring of owners keeps a view", `
type Conn owner struct{ id int }
func (c *Conn) Drop() {}
var kept *Conn
func f() {
	var r ring[Conn, 4]
	r.Push(Conn{id: 1})
	for c := range r.Values() {
		kept = c
	}
}
func main() { f() }`, []string{"cannot pass an element of r to the loop body, which keeps it"}},

	{"range over a ring keeps the frame pointer an element holds", `
var kept *int
func f() {
	x := 42
	var r ring[*int, 4]
	r.Push(&x)
	for _, v := range r {
		kept = v
	}
}
func main() { f() }`, []string{"cannot pass an element of r to the loop body, which keeps it"}},

	{"builder over a local arena returned", `
import (
	"pluto/arena"
	"pluto/strings"
)
func f() string {
	ar := arena.NewFixed(256)
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	return b.String()
}
func main() { _ = f() }`, []string{"cannot return b.String()", "memory allocated from ar, a local arena of f"}},

	{"builder over a local arena printed", `
import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
)
func f() {
	ar := arena.NewFixed(256)
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	fmt.Println(b.String())
}
func main() { f() }`, []string{"cannot convert b.String() to an interface", "was given as an allocator"}},

	{"struct of arena strings converted to an interface", `
import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
)
type pair struct{ a, b string }
func f() {
	ar := arena.NewFixed(256)
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	fmt.Println(pair{b.String(), "y"})
}
func main() { f() }`, []string{"cannot convert pair{...} to an interface"}},

	{"deferred restore in an inner block", `
import (
	"pluto/alloc"
	"pluto/arena"
)
func f(a, b string) string {
	ar := arena.NewFixed(256)
	{
		defer alloc.Use(&ar).Restore() // runs when f returns
	}
	return a + b
}
func main() { _ = f("x", "y") }`, []string{"cannot return a + b"}},

	{"new in a local arena block", `
import (
	"pluto/alloc"
	"pluto/arena"
)
var G *int
func f() {
	ar := arena.NewFixed(256)
	{
		r := alloc.Use(&ar)
		p := new(int)
		G = (*int)(p)
		r.Restore()
	}
}
func main() { f() }`, []string{"cannot store (*int)(p) in package variable G"}},

	{"defaulted allocator in a local arena window", `
import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/vec"
)
func f() vec.Vec[int] {
	ar := arena.NewFixed(256)
	r := alloc.Use(&ar)
	v := vec.New[int]()
	r.Restore()
	return v
}
func main() { _ = f() }`, []string{"cannot return v, which points into memory allocated from ar"}},

	{"view stored in a promoted field of a literal", `
type In struct{ p *int }
type Out struct {
	n int
	In
}
var g Out
func f() {
	x := 1
	g = Out{p: &x}
}
func main() { f() }`, []string{"cannot store Out{...} in package variable g, which outlives x"}},

	// Views of an owner temporary that a pointer method is called on
	// (spec section 11.14).
	{"owner temporary view kept in a local", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func main() {
	s := readAll(3).Slice()
	println(len(s))
}`, []string{"cannot store readAll(3).Slice() in s, which outlives the temporary", "The Buf that readAll(3) returns (at main.p:", "v := readAll(3)\n... v.Slice() ...", "E3009"}},
	{"owner temporary view in a package variable", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
var keep *int
func main() { keep = readAll(1).Ptr() }`, []string{"cannot store readAll(1).Ptr() in package variable keep, which outlives the temporary", "E3009"}},
	{"owner temporary view in an interface", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func main() {
	var i any = readAll(1).Slice()
	_ = i
}`, []string{"cannot store readAll(1).Slice() in i, which outlives the temporary"}},
	{"owner temporary view in a kept literal", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
type H struct{ s []int }
func main() {
	h := &H{s: readAll(1).Slice()}
	_ = h
}`, []string{"cannot store &H{...} in h, which outlives the temporary"}},
	{"owner temporary view in a for init", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func main() {
	for v := readAll(2).Slice(); len(v) > 0; v = v[1:] {
		println(v[0])
	}
}`, []string{"cannot store readAll(2).Slice() in v, which outlives the temporary"}},
	{"owner temporary view returned", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func get() []int { return readAll(2).Slice() }
func main() { _ = get() }`, []string{"cannot return readAll(2).Slice(), which points into the temporary Buf that readAll(2) returns", "before the caller gets readAll(2).Slice()", "Return the Buf itself", "E3009"}},
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
func f() owned[T] {
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
import "pluto/alloc"
func box[T any](x T) any { return x }
func main() { _, _ = box(1), box(alloc.New[int]()) }`},

	// Converting a value of an interface type to another interface copies
	// nothing, so a generic conversion whose instantiations are interfaces and
	// pointers boxes nothing in the frame (go2pu's translation of the walker
	// of internal/syntax/ast).
	{"generic conversion of interface and pointer instantiations", `
type Node interface{ Pos() int }
type Expr interface {
	Node
	exprNode()
}
type Ident struct{ n int }
func (i *Ident) Pos() int { return i.n }
func (i *Ident) exprNode() {}
type Visitor interface{ Visit(n Node) }
type keeper struct{}
var saved Node
func (keeper) Visit(n Node) { saved = n }
func walkList[N Node](v Visitor, list []N) {
	for _, n := range list {
		v.Visit(n)
	}
}
var exprs = []Expr{&Ident{1}}
var idents = []*Ident{{2}}
func main() {
	walkList(Visitor(keeper{}), exprs)
	walkList(Visitor(keeper{}), idents)
}`},

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
import "pluto/alloc"
type E struct{ code int }
func (e *E) Error() string { return "e" }
var saved error
func main() {
	var local E
	var err error = &local
	_ = err.Error()
	err = alloc.New[E]()
	saved = err
}`},

	{"branches join", `
import "pluto/alloc"
var G *int
func main() {
	x := 1
	p := &x
	if x > 0 {
		p = alloc.New[int]()
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
import "pluto/alloc"
type Node struct{ next *Node }
var G *Node
func main() {
	var local Node
	p := &local
	for i := 0; i < 3; i++ {
		_ = p.next
		p = alloc.New[Node]()
	}
	q := alloc.New[Node]()
	for i := 0; i < 2; i++ {
		if i == 1 {
			break
		}
		q = alloc.New[Node]()
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
import "pluto/alloc"
type Pair struct {
	a *int
	b *int
}
var G *int
func main() {
	x := 1
	p := Pair{a: &x, b: alloc.New[int]()}
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
	a := arena.NewFixed(256)
	r := alloc.Use(&a)
	p := new(int)
	*p = 1
	r.Restore()
}`},

	{"pointer-shaped struct holding heap memory returned as an error", `
import "pluto/alloc"
type W struct{ p *int }
func (W) Error() string { return "w" }
func f() error { return W{alloc.New[int]()} }
func main() { _ = f() }`},
	{"heap-only fields of a chunk list over a frame-backed parent", `
import (
	"pluto/alloc"
	"unsafe"
)
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
func (heap) Alloc(n uintptr) unsafe.Pointer { return unsafe.Pointer(alloc.New[[64]byte]()) }
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
	{"closures returned in the caller's region", `
func f() func() int {
	n := 0
	return func() int { n++; return n }
}
type C struct{ n int }
func (c C) Get() int { return c.n }
func g(c C) func() int { return c.Get }
func h(k int) func() int {
	if k < 0 {
		panic("k")
	}
	return f()
}
func main() {
	a, b, c := f(), g(C{1}), h(2)
	println(a(), b(), c())
}`},
	{"returning what calling a call's closure returns", `
func inc(n int) func() int { return func() int { n++; return n } }
func own(n int) func() func() int {
	inner := inc(n)
	return func() func() int { return inner }
}
func unwrap(n int) func() int {
	p := own(n)
	return p()
}
func main() {
	u := unwrap(10)
	println(u(), u())
}`},
	{"calling a region's closure that returns a counted string it captures", `
var saved string
func greeter(name string) func() string {
	greeting := "hi " + name
	return func() string { return greeting }
}
func h() { saved = greeter("x")() }
func main() { h(); println(saved) }`},
	{"closure returned in the caller's region using what it captures", `
func mk(p *int) func() int {
	x := 3
	q := &x
	return func() int { return *p + *q + x }
}
func main() {
	n := 1
	println(mk(&n)())
}`},
	{"task statement copies packed variadic arguments", `
func sum(done chan int, xs ...int) {
	t := 0
	for _, x := range xs {
		t += x
	}
	done <- t
}
func main() {
	d := make(chan int)
	task sum(d, 1, 2, 3)
	println(<-d)
}`},
	{"constructor yielding copies of what it captures", `
import "pluto/alloc"
var saved *int
func vals(p *int) func(func(*int) bool) {
	return func(yield func(*int) bool) { yield(p) }
}
func main() {
	for p := range vals(alloc.New[int]()) {
		saved = p
	}
}`},
	{"variadic call in a package initialiser", `
func all(xs ...int) func() []int {
	return func() []int { return xs }
}
var F = all(1, 2)
func main() { println(len(F())) }`},
	{"type parameter call reaches only methods of matching signature", `
type W interface{ Write(p []byte) int }
type Dev[T ~uint8] struct{ data T }
func (d *Dev[T]) Write(v T) int { d.data = v; return 1 }
type sink struct{}
func (sink) Write(p []byte) int { return len(p) }
func put[V W](w V) int {
	var buf [4]byte
	return w.Write(buf[:])
}
func main() {
	var d Dev[uint8]
	d.Write(1)
	println(put(sink{}))
}`},
	{"boxes returned and stored themselves", `
type N struct{ v int }
var G owned[N]
var S shared[N]
func f() owned[N] {
	n := new(N)
	n.v = 1
	return n
}
func main() {
	G = f()
	S = nil
	p := (*N)(G)
	_ = p.v
}`},
	{"allocations after the arena window", `
import (
	"pluto/alloc"
	"pluto/arena"
)
func g(a, b string) string {
	ar := arena.NewFixed(256)
	{
		r := alloc.Use(&ar)
		_ = a + b
		r.Restore()
	}
	return a + b // after the window's block
}
func main() { _ = g("x", "y") }`},
	{"arena text written as bytes or measured", `
import (
	"pluto/arena"
	"pluto/fmt"
	"pluto/os"
	"pluto/strings"
)
func f(a, b string) int {
	ar := arena.NewFixed(256)
	var out strings.Builder
	out.Init(&ar)
	out.WriteString("x")
	os.Stdout.Write(out.Bytes())
	n := len(out.String())
	println(out.String())
	var heap strings.Builder
	defer heap.Free()
	heap.WriteString("y")
	fmt.Println(heap.String(), n)
	return n
}
func main() { _ = f("x", "y") }`},
	{"arena text converted after the window", `
import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
)
func f(a, b string) {
	ar := arena.NewFixed(256)
	{
		r := alloc.Use(&ar)
		_ = a + b
		r.Restore()
	}
	fmt.Println(a + b) // made after the window: not in the arena
}
func main() { f("x", "y") }`},
	{"text in a manual arena or a custom allocator converted", `
import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
	"unsafe"
)
type counting struct{ next alloc.Allocator }
func (c *counting) Alloc(n, a uintptr) unsafe.Pointer       { return c.next.Alloc(n, a) }
func (c *counting) Free(p unsafe.Pointer, n, a uintptr)     { c.next.Free(p, n, a) }
func f(buf []byte) {
	ar := arena.NewUnsafe(buf) // manual: the program owns its lifetime
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	fmt.Println(b.String())
	c := counting{next: alloc.Default()}
	r := alloc.Use(&c)
	s := "a" + b.String()
	fmt.Println(s)
	r.Restore()
}
var mem [4096]byte
func main() { f(mem[:]) }`},
	{"builder over a heap allocator printed", `
import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/strings"
)
func f() string {
	var b strings.Builder
	b.Init(alloc.Default())
	defer b.Free()
	b.WriteString("x")
	fmt.Println(b.String())
	return b.String()
}
func main() { _ = f() }`},
	{"explicit allocator in an arena window", `
import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/vec"
)
func f() vec.Vec[int] {
	heap := alloc.Default()
	ar := arena.NewFixed(256)
	defer alloc.Use(&ar).Restore()
	v := vec.New[int](heap)
	v.Push(1)
	return v
}
func main() { _ = f() }`},
	{"heap arena installed", `
import (
	"pluto/alloc"
	"pluto/arena"
)
var ar arena.Chunked
func f(a, b string) string {
	defer alloc.Use(&ar).Restore()
	return a + b
}
func main() { _ = f("x", "y") }`},
	{"type assertion and a method that keeps its receiver", `
import "pluto/alloc"
type Stringer interface{ String() string }
type Named struct{ name string }
var last *Named
func (n *Named) String() string {
	last = n
	return "named"
}
// A value method of Wide is called on a Wide box, which the pointer method of
// Named never gets: the receiver type narrows what the type assertion left.
type Wide struct{ a, b, c int }
func (w Wide) String() string { return "wide" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func main() {
	n := alloc.New[Named]()
	println(call(n))
	println(call(Wide{1, 2, 3}))
}`},
	{"function literal and a method value that keeps its receiver", `
import "pluto/alloc"
type S struct{ n int }
var keepS *S
func (s *S) visit(x int) bool {
	keepS = s
	return x > 0
}
func call(f func(int) bool) bool { return f(1) }
func main() {
	s := alloc.New[S]()
	_ = call(s.visit)
	total := 0
	_ = call(func(x int) bool {
		total += x
		return true
	})
	println(total)
}`},
	{"owner temporary views within the statement", `
type Buf owner struct {
	data [4]int
	n    int
}
func (b *Buf) Slice() []int { return b.data[:b.n] }
func (b *Buf) Ptr() *int { return &b.data[0] }
func readAll(n int) Buf {
	var b Buf
	b.n = n
	return b
}
func sum(s []int) int {
	t := 0
	for _, v := range s {
		t += v
	}
	return t
}
func main() {
	println(sum(readAll(3).Slice()), len(readAll(2).Slice()), *readAll(1).Ptr())
	if s := readAll(3).Slice(); len(s) > 0 {
		println(s[0])
	}
	switch s := readAll(2).Slice(); len(s) {
	case 2:
		println(s[1])
	}
	for _, v := range readAll(2).Slice() {
		println(v)
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
