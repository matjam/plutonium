// Ported from internal/build/counted_errors_test.go at the go-cutover tag.

package tests

import (
	"testing"
)

// TestCountedErrors checks section 16.7: error interfaces keep counted
// references alive through returns, copies, overwrites and or clauses.
func TestCountedErrors(t *testing.T) {
	t.Parallel()
	const src = `package main

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/fmt"
)

var drops int

type detail owner struct { text string }
func (e *detail) Error() string { return e.text }
func (e *detail) Drop() { drops++ }

type contextual interface { error; Context() string }
func (e *detail) Context() string { return e.text }

func makeError(text string) error {
	e := alloc.Synced[detail]()
	e.text = text
	return e
}

func fail() (int, error) { return 0, makeError("failure") }
func propagate() error { _ = fail() or return; return nil }
func discard() { _ = fail() or { return } }

func copies() {
	e := makeError("first")
	fmt.Println(e)
	f := e
	var ptr *detail
	println(errors.As(f, &ptr), ptr.Error())
	e = makeError("second")
	println(f.Error(), e.Error(), drops)
	e = nil
	println(drops)
	c := f.(contextual)
	f = nil
	println(c.Context(), drops)
	var borrowed any = c
	println(borrowed.(interface { Error() string }).Error())
}

func main() {
	copies()
	println(drops)
	discard()
	println(drops)
	{
		e := propagate()
		println(e.Error(), drops)
	}
	println(drops)
}
`
	for _, level := range []int{0, 2} {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			t.Parallel()
			exe := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, exe, "")
			const want = "first\ntrue first\nfirst second 0\n1\nfirst 1\nfirst\n2\n3\nfailure 3\n4\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}

// Counted errors in containers and wrapped trees keep independent references.
func TestCountedErrorContainers(t *testing.T) {
	t.Parallel()
	const src = `package main
import (
 "pluto/alloc"
 "pluto/errors"
 "pluto/fmt"
)
var drops int
type E owner struct { n int }
func (e *E) Error() string { return "error" }
func (e *E) Drop() { drops++ }
func makeError(n int) error { e := alloc.Synced[E](); e.n = n; return e }
func containers() {
 e := makeError(7)
 a := [5]error{e, e, e, e, e}
 m := make(map[int]error)
 m[1] = e
 c := make(chan error, 1)
 c <- e
 e = nil
 clear(a[:])
 delete(m, 1)
 e = <-c
 var p *E
 println(errors.As(e, &p), p.n, drops)
 _, exact := e.(*E)
 println(exact)
 var held error
 println(errors.As(e, &held))
 e = nil
 println(held.Error(), drops)
 held = nil
 println(drops)
}

func wrapping() {
 e := makeError(8)
 wrapped := fmt.Errorf("wrap: %w", e)
 joined := errors.Join(wrapped, errors.New("sentinel"))
 e, wrapped = nil, nil
 var p *E
 println(errors.As(joined, &p), p.n, drops)
 println(joined.Error())
}
func syncedBox() {
 p := alloc.Synced[E]()
 var e error = p
 p = nil
 var q *E
 println(errors.As(e, &q), q.n, drops)
 e = nil
 println(drops)
}
func main() {
 containers()
 wrapping()
 println(drops)
 syncedBox()
}
`
	for _, level := range []int{0, 2} {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			t.Parallel()
			exe := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, exe, "")
			const want = "true 7 0\nfalse\ntrue\nerror 0\n1\ntrue 8 1\nwrap: error\nsentinel\n2\ntrue 0 2\n3\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}

// Independent counted targets must survive the temporary errors visited
// by As, and exact assertions retain shared and synced dynamic references.
func TestCountedErrorTargets(t *testing.T) {
	t.Parallel()
	const src = `package main
import (
 "pluto/alloc"
 "pluto/errors"
)
var drops int
type E owner struct { n int }
func (e *E) Error() string { return "error" }
func (e *E) Drop() { drops++ }
type root int
func (root) Error() string { return "root" }
func (root) Unwrap() error { p := alloc.Synced[E](); p.n = 42; return p }
func extract() synced[E] { var p synced[E]; println(errors.As(root(0), &p)); return p }
func asserted() synced[E] { var e error = alloc.Synced[E](); return e.(synced[E]) }
func syncedTarget() synced[E] { var e error = alloc.Synced[E](); return e.(synced[E]) }
func run() {
 p := extract()
 println(p.n, drops)
 p = nil
 println(drops)
 q := asserted()
 println(q.n, drops)
 q = nil
 println(drops)
 s := syncedTarget()
 println(s.n, drops)
 s = nil
 println(drops)
}
func main() { run() }
`
	for _, level := range []int{0, 2} {
		t.Run(string(rune('0'+level)), func(t *testing.T) {
			t.Parallel()
			exe := buildDebugAlloc(t, src, level)
			got, status := runDebugAlloc(t, exe, "")
			const want = "true\n42 0\n1\n0 1\n2\n0 2\n3\n"
			if status != 0 || got != want {
				t.Fatalf("exit %d, output %q; want %q", status, got, want)
			}
		})
	}
}
