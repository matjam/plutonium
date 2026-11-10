package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Section 17.3, abi.md 12.10 (roadmap item 158): a task that exits with
// rt.ExitTask in the middle of a statement drops the statement's finished
// temporaries and then the frames' locals, and a task that exits inside a
// Drop method finishes destroying that value (its fields) before the rest.
// Each case runs on a task of a sync.Scope, which joins it; the debug
// allocator reports anything left undropped.
func TestTaskExitDrops(t *testing.T) {
	t.Parallel()
	const source = `package main
import ("pluto/rt"; "pluto/sync")
const exit = EXIT
type Res owner struct { id int }
func (r *Res) Drop() { if r.id != 0 { println("drop", r.id) } }
type Inner owner struct { id int }
func (r *Inner) Drop() { println("inner", r.id) }
type Outer owner struct { a, b Inner }
func (o *Outer) Drop() {
	println("outer")
	if exit { rt.ExitTask() }
	println("outer returns")
}
func make1() Res { return Res{id: 1} }
func g() int {
	if exit { println("g exits"); rt.ExitTask() }
	println("g returns")
	return 0
}
func f(r Res, n int) { println("f", r.id) }
func h(r Res) {
	println("h", r.id)
	if exit { rt.ExitTask() }
	println("h returns")
}
// The spec's example: g exits after make1's result is made.
func operands() {
	var x Res
	x.id = 2
	f(make1(), g())
	println("after f")
}
func callee() {
	var x Res
	x.id = 2
	h(make1())
	println("after h")
}
func inDrop() {
	var y Res
	y.id = 3
	o := Outer{a: Inner{id: 1}, b: Inner{id: 2}}
	o.a.id = 1
	println("body")
}
func run(name string, fn func()) {
	println(name)
	{
		var s sync.Scope
		s.Task(func() { fn() })
	}
	println("joined")
}
func main() {
	run("operands", operands)
	run("callee", callee)
	run("inDrop", inDrop)
}
`
	exiting := "operands\ng exits\ndrop 1\ndrop 2\njoined\n" +
		"callee\nh 1\ndrop 1\ndrop 2\njoined\n" +
		"inDrop\nbody\nouter\ninner 1\ninner 2\ndrop 3\njoined\n"
	// The twin: the same code with no exit drops as it returns.
	returning := "operands\ng returns\nf 1\ndrop 1\nafter f\ndrop 2\njoined\n" +
		"callee\nh 1\nh returns\ndrop 1\nafter h\ndrop 2\njoined\n" +
		"inDrop\nbody\nouter\nouter returns\ninner 1\ninner 2\ndrop 3\njoined\n"
	for _, exit := range []bool{true, false} {
		want := returning
		if exit {
			want = exiting
		}
		src := strings.Replace(source, "EXIT", fmt.Sprint(exit), 1)
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("exit%t/O%d", exit, opt), func(t *testing.T) {
				t.Parallel()
				exe := buildDebugAlloc(t, src, opt)
				out, status := runDebugAlloc(t, exe, "")
				if status != 0 || out != want {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, want)
				}
			})
		}
	}
}
