package escape_test

import (
	"strings"
	"testing"
)

// Programs that keep a pointer into a dead stack frame by a path the exact
// type filters of the analysis once took for impossible (the adversarial review
// of commits 6002fd24 to 77263d8b). Each must be reported.
var keepsFrameWays = []struct {
	name string
	src  string
	want string // a substring of the first message
}{
	{"closure converted to a named func type with methods", `
type Stringer interface{ String() string }
type HF func() int
var keepF HF
func (h HF) String() string { keepF = h; return "hf" }
func call(s Stringer) string { return s.String() }
func f() {
	x := 42
	println(call(HF(func() int { return x })))
}
func main() { f() }`, "package variable keepF"},
	{"method value of a closure converted to a named func type", `
type Stringer interface{ String() string }
type HF func() int
type C struct{ n int }
func (c *C) get() int { return c.n }
var keepF HF
func (h HF) String() string { keepF = h; return "hf" }
func call(s Stringer) string { return s.String() }
func f() {
	var c C
	println(call(HF(c.get)))
}
func main() { f() }`, "package variable keepF"},
	// The third adversarial review: types converted to an interface are told
	// apart by identity, not by how they print.
	{"pointer to a type parameter converted after another of the same name", `
type Stringer interface{ String() string }
type S struct{ n int }
var keepS *S
func (s *S) String() string { keepS = s; return "s" }
func first[T any](v *T) any { return any(v) }
func show[T any](v *T) string {
	if s, ok := any(v).(Stringer); ok {
		return s.String()
	}
	return ""
}
func f() {
	var n int
	_ = first(&n)
	s := S{42}
	println(show(&s))
}
func main() { f() }`, "cannot pass &s to show"},
	{"local types of one name in two functions", `
type Stringer interface{ String() string }
type T0 struct{ p *int }
var keep *int
func (t T0) String() string { keep = t.p; return "t0" }
func call(s Stringer) string { return s.String() }
func use(v any) {}
func one() {
	type L struct{ n int }
	use(L{1})
}
func f() {
	type L struct{ T0 }
	x := 42
	println(call(L{T0{&x}}))
}
func main() { one(); f() }`, "cannot pass L{...} to call"},
	// The third adversarial review: the body of a range loop over a function
	// is the yield function the loop passes (section 12.11).
	{"an iterator yields its own variable to a body that keeps it", `
var keep *int
func seq(yield func(*int) bool) {
	x := 42
	yield(&x)
}
func f() {
	var last *int
	for p := range seq {
		last = p
	}
	keep = last
}
func main() { f() }`, "cannot pass &x to yield (which may call the loop body at main.p:10)"},
	{"an iterator keeps yield", `
var keepY func(*int) bool
func seq(yield func(*int) bool) { keepY = yield }
func f() {
	n := 1
	for p := range seq {
		n += *p
	}
}
func main() { f() }`, "cannot pass the loop body to seq, which keeps it"},
	{"a container's All method yields its own variable", `
import "pluto/iter"
type C struct{ n int }
func (c *C) All() iter.Seq2[int, *int] {
	return func(yield func(int, *int) bool) {
		local := c.n
		yield(0, &local)
	}
}
var keep *int
func f() {
	c := C{1}
	for _, p := range c {
		keep = p
	}
}
func main() { f() }`, "cannot pass &local to yield"},
	// The third adversarial review: the functions the compiler expands inline
	// have summaries of what the expansion does, not of their placeholder
	// bodies.
	{"vec.Vec Push into a package variable", `
import "pluto/vec"
var gv vec.Vec[*int]
func f() {
	x := 42
	gv.Push(&x)
}
func main() { f() }`, "cannot pass &x to vec.(*Vec).Push"},
	{"rt.InitValue into a package variable", `
import "pluto/rt"
var g *int
func f() {
	x := 42
	rt.InitValue(&g, &x)
}
func main() { f() }`, "cannot pass &x to rt.InitValue"},
	{"rt.CopyValue of a pointer to a frame pointer", `
import "pluto/rt"
var g *int
func f() {
	x := 42
	p := &x
	g = rt.CopyValue(&p)
}
func main() { f() }`, "cannot store rt.CopyValue(&p) in package variable g"},
	{"rt.MoveValues into a package array", `
import "pluto/rt"
var g [1]*int
func f() {
	x := 42
	p := &x
	rt.MoveValues(&g[0], &p, 1)
}
func main() { f() }`, "cannot pass &p to rt.MoveValues"},
	{"strings.Slice of a view of the frame", `
import (
	"pluto/strings"
	"unsafe"
)
func f() string {
	b := [2]byte{'o', 'k'}
	return strings.Slice(unsafe.String(&b[0], 2), 0, 1)
}
func main() { println(f()) }`, "cannot return strings.Slice"},
	// A callee that stores heap memory into its caller's memory: what the
	// caller then stores through it is stored in the heap.
	{"vec.Vec Push on a local vector", `
import "pluto/vec"
var keep *int
func f() {
	x := 42
	v := vec.New[*int]()
	v.Push(&x)
	keep = v.At(0)
}
func main() { f() }`, "cannot pass &x to vec.(*Vec).Push"},
	{"store through a field a callee set to heap memory", `
var heapArr [1]*int
type B struct{ p *[1]*int }
func (b *B) init() { b.p = &heapArr }
func f() {
	var b B
	b.init()
	x := 42
	b.p[0] = &x
}
func g() int {
	var arr [1]*int
	b := B{&arr}
	return len(b.p)
}
func main() { f(); println(g()) }`, "cannot store &x in b.p[0]"},
	{"element of an array as a pointer in an interface", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(s Stringer) string { return s.String() }
func f() {
	var arr [2]S
	println(call(&arr[1]))
}
func main() { f() }`, "package variable keepS"},
	{"element of an array, through an assertion", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func f() {
	var arr [2]S
	println(call(&arr[1]))
}
func main() { f() }`, "package variable keepS"},
	{"pointer converted to another named type", `
type S struct{ n int }
type B struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(s Stringer) string { return s.String() }
func f() {
	var b B
	println(call((*S)(&b)))
}
func main() { f() }`, "package variable keepS"},
	{"method value of an element of an array", `
type S struct{ n int }
var keepS *S
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(f func(int) bool) bool { return f(1) }
func f() {
	var arr [2]S
	_ = call(arr[1].visit)
}
func main() { f() }`, "package variable keepS"},
	{"method value of an element of a slice of an array", `
type S struct{ n int }
var keepS *S
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(f func(int) bool) bool { return f(1) }
func f() {
	var arr [2]S
	sl := arr[:]
	_ = call(sl[1].visit)
}
func main() { f() }`, "package variable keepS"},
	{"method value of a converted pointer", `
type S struct{ n int }
type B struct{ n int }
var keepS *S
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(f func(int) bool) bool { return f(1) }
func f() {
	var b B
	_ = call((*S)(&b).visit)
}
func main() { f() }`, "package variable keepS"},
	{"method value stored in a variable first", `
type S struct{ n int }
var keepS *S
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(f func(int) bool) bool { return f(1) }
func f() {
	var arr [2]S
	g := arr[0].visit
	_ = call(g)
}
func main() { f() }`, "package variable keepS"},
	{"struct of one pointer in an interface variable", `
type Inner struct{ n int }
var keep *Inner
type Stringer interface{ String() string }
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
func f() {
	var in Inner
	var s Stringer = D{&in}
	println(s.String())
}
func main() { f() }`, "package variable keep"},
	{"struct of one pointer through an assertion", `
type Inner struct{ n int }
var keep *Inner
type Stringer interface{ String() string }
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
func call(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}
func f() {
	var in Inner
	println(call(D{&in}))
}
func main() { f() }`, "package variable keep"},
	{"a union that holds a pointer, stored in a package variable", `
type Inner struct{ n int }
type U union {
	None
	P *Inner
}
var keep U
func f() {
	var in Inner
	keep = U.P(&in)
}
func main() { f() }`, "package variable keep"},
	{"a union with a struct variant that holds a pointer", `
type Inner struct{ n int }
type U union {
	None
	S struct{ p *Inner }
}
var keep U
func f() {
	var in Inner
	keep = U.S{p: &in}
}
func main() { f() }`, "package variable keep"},
	{"the payload of a union variant, kept", `
type Inner struct{ n int }
type U union {
	None
	P *Inner
}
var keep *Inner
func f() {
	var in Inner
	u := U.P(&in)
	switch v := u.(type) {
	case P:
		keep = v
	default:
	}
}
func main() { f() }`, "package variable keep"},
	{"a struct type with no name, passed through a function", `
type Inner struct{ n int }
var keep *Inner
func id(s struct{ p *Inner }) struct{ p *Inner } { return s }
func f() {
	var in Inner
	u := struct{ p *Inner }{&in}
	v := id(u)
	keep = v.p
}
func main() { f() }`, "package variable keep"},
	{"the payload of a union variant that is a struct type with no name", `
type Inner struct{ n int }
type U union {
	None
	S struct{ p *Inner }
}
var keep *Inner
func f() {
	var in Inner
	u := U.S{p: &in}
	switch v := u.(type) {
	case S:
		keep = v.p
	default:
	}
}
func main() { f() }`, "package variable keep"},
	{"a method of a struct field that fmt calls, keeping what the value holds", `
import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
type W struct {
	P   *Inner
	pad int
}
func (w W) String() string { keep = w.P; return "w" }
type Outer struct{ W W }
func f() {
	var in Inner
	fmt.Println(Outer{W{&in, 1}})
}
func main() { f() }`, "fmt.Println"},
	{"a method of an element of a slice that fmt calls", `
import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
func (i *Inner) String() string { keep = i; return "inner" }
func f() {
	var in Inner
	fmt.Println([]*Inner{&in})
}
func main() { f() }`, "fmt.Println"},
	{"a method of a pointer-shaped struct that fmt calls", `
import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
type Outer struct{ D D }
func f() {
	var in Inner
	fmt.Println(Outer{D{&in}})
}
func main() { f() }`, "fmt.Println"},
	{"a union payload that fmt walks", `
import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
func (i *Inner) String() string { keep = i; return "inner" }
type U union {
	None
	P *Inner
}
func f() {
	var in Inner
	fmt.Println(U.P(&in))
}
func main() { f() }`, "fmt.Println"},
	{"an element a callee takes of the array its parameter points to", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(p *[2]S) string { return call(&p[1]) }
func f() {
	var arr [2]S
	println(g(&arr))
}
func main() { f() }`, "keepS"},
	{"a method value of an element a callee takes", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(p *[2]S) bool { return callf(p[1].visit) }
func f() {
	var arr [2]S
	println(g(&arr))
}
func main() { f() }`, "keepS"},
	{"an element of a slice parameter", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(s []S) string { return call(&s[1]) }
func f() {
	var arr [2]S
	println(g(arr[:]))
}
func main() { f() }`, "keepS"},
	{"every element of a slice parameter, in a range loop", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func each(items []S) string {
	out := ""
	for i := range items {
		var s Stringer = &items[i]
		out += s.String()
	}
	return out
}
func f() {
	items := [3]S{}
	println(each(items[:]))
}
func main() { f() }`, "keepS"},
	{"an element of a slice a callee makes of its parameter", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(p *[2]S) string {
	sl := p[:]
	return call(&sl[1])
}
func f() {
	var arr [2]S
	println(g(&arr))
}
func main() { f() }`, "keepS"},
	{"an element taken two calls down", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func h(p *[2]S) string { return call(&p[1]) }
func g(p *[2]S) string { return h(p) }
func f() {
	var arr [2]S
	println(g(&arr))
}
func main() { f() }`, "keepS"},
	{"an element of the receiver of a method of a named array", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
type Arr [2]S
func (a *Arr) first() string { return call(&a[1]) }
func f() {
	var arr Arr
	println(arr.first())
}
func main() { f() }`, "keepS"},
	{"an element a callee takes after a type assertion", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(a any) string {
	if p, ok := a.(*[2]S); ok {
		return call(&p[1])
	}
	return ""
}
func f() {
	var arr [2]S
	println(g(&arr))
}
func main() { f() }`, "keepS"},
	{"an element a callee prints with fmt", `
import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
func (i *Inner) String() string { keep = i; return "inner" }
func g(p *[2]Inner) { fmt.Println(&p[1]) }
func f() {
	var arr [2]Inner
	g(&arr)
}
func main() { f() }`, "fmt.Println"},
	{"a union variant as a function value", `
type Inner struct{ n int }
var keep *Inner
type U union {
	None
	P *Inner
}
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}
func f() {
	var in Inner
	mk := U.P
	store(mk(&in))
}
func main() { f() }`, "cannot pass mk(&in) to store"},
	{"a union variant passed as a function value", `
type Inner struct{ n int }
var keep *Inner
type U union {
	None
	P *Inner
}
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}
func apply(mk func(*Inner) U, p *Inner) U { return mk(p) }
func f() {
	var in Inner
	store(apply(U.P, &in))
}
func main() { f() }`, "cannot pass apply(U.P, &in) to store"},
	{"a union made by a variant function value, returned", `
type Inner struct{ n int }
var keep *Inner
type U union {
	None
	P *Inner
}
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}
func g() U {
	var in Inner
	mk := U.P
	return mk(&in)
}
func main() { store(g()) }`, "cannot return mk(&in)"},
	{"a union variant function value in a struct field, returned", `
type Inner struct{ n int }
var keep *Inner
type U union {
	None
	P *Inner
}
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}
type H struct{ mk func(*Inner) U }
func g() U {
	var in Inner
	h := H{U.P}
	return h.mk(&in)
}
func main() { store(g()) }`, "cannot return h.mk(&in)"},
	{"a variant of a generic union as a function value", `
type Inner struct{ n int }
var keep *Inner
type Opt[T any] union {
	None
	Some T
}
func store(o Opt[*Inner]) {
	if p, ok := o.(Opt[*Inner].Some); ok {
		keep = p
	}
}
func f() {
	var in Inner
	mk := Opt[*Inner].Some
	store(mk(&in))
}
func main() { f() }`, "cannot pass mk(&in) to store"},
	{"an element of a pointer converted to an array of a type with methods", `
import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func f() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	println(call(&q[1]))
}
func main() { f() }`, "cannot pass &q[1] to call"},
	{"a method value of an element of a converted pointer", `
import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func f() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	println(callf(q[1].visit))
}
func main() { f() }`, "cannot pass q[1].visit to callf"},
	{"an element of a pointer a callee converts", `
import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func g(p *[2]int) string {
	q := (*[2]S)(unsafe.Pointer(p))
	return call(&q[1])
}
func f() {
	var x [2]int
	println(g(&x))
}
func main() { f() }`, "cannot pass &x to g"},
	{"an element of a slice of a converted pointer", `
import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
func f() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	sl := q[:]
	println(call(&sl[1]))
}
func main() { f() }`, "cannot pass &sl[1] to call"},
	{"a field of a struct of a converted pointer", `
import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func (s *S) visit(x int) bool { keepS = s; return x > 0 }
func call(s Stringer) string { return s.String() }
func callf(f func(int) bool) bool { return f(1) }
type B struct{ a, b S }
func f() {
	var x [2]int
	q := (*B)(unsafe.Pointer(&x))
	println(call(&q.b))
}
func main() { f() }`, "cannot pass &q.b to call"},
}

func TestKeepsFrameWays(t *testing.T) {
	for _, c := range keepsFrameWays {
		t.Run(c.name, func(t *testing.T) {
			err := check(t, "package main\n"+c.src+"\n")
			if err == nil {
				t.Fatal("no escape reported")
			}
			if msg := fullText(err); !strings.Contains(msg, c.want) {
				t.Errorf("the message does not contain %q:\n%s", c.want, msg)
			}
		})
	}
}

// What the exact filters know stays known: a pointer to a variable of one
// type is not a receiver of another type's method that keeps it, and an
// interface holding a value that holds no such pointer calls no such method.
var staysExact = []struct{ name, src string }{
	{"a local vector of pointers to package variables", `
import "pluto/vec"
var a, b int
func f() int {
	v := vec.New[*int]()
	v.Push(&a)
	v.Push(&b)
	p := v.At(1)
	x := 3
	q := &x
	return *p + *q + v.Len()
}
func main() { println(f()) }`},
	{"a loop body that keeps what a vector yields in a variable of its function", `
import "pluto/vec"
var a, b int
func f() int {
	v := vec.New[*int]()
	defer v.Free()
	v.Push(&a)
	v.Push(&b)
	var last *int
	for _, p := range v.All() {
		last = p
	}
	return *last
}
func main() { println(f()) }`},
	{"a loop body that uses its own variable's address while yield runs", `
func seq(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}
func use(p *int) int { return *p }
func f() int {
	sum := 0
	for x := range seq {
		y := x
		sum += use(&y)
	}
	return sum
}
func main() { println(f()) }`},
	{"a pointer to a whole variable of another type", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
type Plain struct{ n int }
func (p *Plain) String() string { return "plain" }
func call(s Stringer) string { return s.String() }
func f() {
	var p Plain
	println(call(&p))
}
func main() { f() }`},
	{"an element of an array of another type", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
type Plain struct{ n int }
func (p *Plain) String() string { return "plain" }
func call(s Stringer) string { return s.String() }
func f() {
	var arr [2]Plain
	println(call(&arr[1]))
}
func main() { f() }`},
	{"fmt and a method that keeps its receiver, for values that hold none", `
import "pluto/fmt"
type Named struct{ name string }
var last *Named
func (n *Named) String() string { last = n; return "named" }
type Pair struct {
	a int
	s string
}
func f() {
	var p Pair
	fmt.Println(1, "x", 2.5, p, []int{1, 2}, [2]string{"a", "b"})
}
func main() { f() }`},
	{"a union that holds no pointer, printed", `
import "pluto/fmt"
type Named struct{ name string }
var last *Named
func (n *Named) String() string { last = n; return "named" }
type U union {
	None
	N int
}
func f() {
	fmt.Println(U.N(1))
}
func main() { f() }`},
	{"an element a callee takes of an array of another type", `
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
type Plain struct{ n int }
func (p *Plain) String() string { return "plain" }
func call(s Stringer) string { return s.String() }
func g(p *[2]Plain) string { return call(&p[1]) }
func f() {
	var arr [2]Plain
	println(g(&arr))
}
func main() { f() }`},
	{"fmt and a value that holds a type with a method that keeps, nil", `
import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
type W struct {
	P   *Inner
	pad int
}
func (w W) String() string { keep = w.P; return "w" }
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
type Outer struct{ W W }
func f() {
	fmt.Println(Outer{W{nil, 0}})
}
func main() { f() }`},
	{"a union variant as a function value, of a package variable's address", `
type Inner struct{ n int }
var keep *Inner
var global Inner
type U union {
	None
	P *Inner
}
func store(u U) {
	if v, ok := u.(U.P); ok {
		keep = v
	}
}
func f() {
	mk := U.P
	store(mk(&global))
}
func main() { f() }`},
	{"an element of a pointer converted to an array of a type without methods", `
import "unsafe"
type S struct{ n int }
var keepS *S
type Stringer interface{ String() string }
func (s *S) String() string { keepS = s; return "s" }
func call(s Stringer) string { return s.String() }
func sum(p *int) int { return *p + 1 }
func f() {
	var x [2]S
	q := (*[2]int)(unsafe.Pointer(&x))
	println(sum(&q[1]))
}
func main() { f() }`},
}

func TestStaysExact(t *testing.T) {
	for _, c := range staysExact {
		t.Run(c.name, func(t *testing.T) {
			if err := check(t, "package main\n"+c.src+"\n"); err != nil {
				t.Fatalf("reported:\n%s", fullText(err))
			}
		})
	}
}
