package tests

import (
	"fmt"
	"testing"
)

// Section 18.8 (finding C7; roadmap item 160): the array behind a variadic
// argument list is frame storage of the caller, and its counted contents
// are released when the function returns, after its deferred calls, or when
// the same expression runs again and refills it. Run with the debug
// allocator, whose leak report catches contents never released.
func TestVariadicArrayRelease(t *testing.T) {
	t.Parallel()
	const prelude = `package main
import ("pluto/alloc"; "pluto/strings")
type Res owner struct { id int }
func (r *Res) Drop() { println("drop", r.id) }
func mk(i int) shared[Res] { p := alloc.Shared[Res](); p.id = i; return p }
func f(xs ...shared[Res]) { println("call", len(xs), xs[0].id) }
func fs(xs ...string) int { return len(xs[0]) }
`
	cases := []struct{ name, source, output string }{
		// Each pass refills the array, releasing what the pass before
		// put there; the last pass's contents go when run returns.
		{"loop", prelude + `func run() {
	for i := 1; i <= 3; i++ {
		f(mk(i))
		println("after", i)
	}
	println("end")
}
func main() {
	run()
	println("returned")
}
`, "call 1 1\nafter 1\ndrop 1\ncall 1 2\nafter 2\ndrop 2\ncall 1 3\nafter 3\nend\ndrop 3\nreturned\n"},
		// The contents outlive the function's deferred calls.
		{"deferred", prelude + `func run() {
	defer println("deferred")
	f(mk(7))
	println("body")
}
func main() {
	run()
}
`, "call 1 7\nbody\ndeferred\ndrop 7\n"},
		// Strings built at run time are released the same way.
		{"strings", prelude + `func run() int {
	n := 0
	for i := 0; i < 100; i++ {
		n += fs(strings.Repeat("s", 40+i), strings.Repeat("t", 3))
	}
	return n
}
func main() {
	println(run())
}
`, "8950\n"},
		// The twin: a variadic list of plain values.
		{"plainValues", prelude + `func sum(xs ...int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}
func main() {
	t := 0
	for i := 0; i < 3; i++ {
		t += sum(i, i, i)
	}
	println(t)
}
`, "9\n"},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				exe := buildDebugAlloc(t, dropUnusedStrings(c.source), opt)
				out, status := runDebugAlloc(t, exe, "")
				if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}
