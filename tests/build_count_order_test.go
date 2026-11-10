package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Sections 13.2 and 18.8, abi.md 3.3 (findings B10 and B18; roadmap item
// 159): copy counts the source before it releases the destination, so a copy
// of a slice onto itself or onto an overlapping slice frees nothing still in
// use; and a counted parameter is borrowed, so the first assignment of a new
// value to it releases nothing of the caller's. Run with the debug allocator,
// whose leak report and poisoned frees catch a release in the wrong order.
func TestCountBeforeRelease(t *testing.T) {
	t.Parallel()
	const prelude = `package main
import ("pluto/alloc"; "pluto/strings")
type Res owner struct { id int }
func (r *Res) Drop() { println("drop", r.id) }
func mk(i int) shared[Res] { p := alloc.Shared[Res](); p.id = i; return p }
`
	cases := []struct{ name, source, output string }{
		// The array holds the only count of each element.
		{"copyOntoItself", prelude + `func main() {
	var a [2]shared[Res]
	a[0], a[1] = mk(1), mk(2)
	n := copy(a[:], a[:])
	println(n, a[0].id, a[1].id)
	copy(a[1:], a[:1])
	println(a[0].id, a[1].id)
	copy(a[:1], a[1:])
	println("end")
}
`, "2 1 2\ndrop 2\n1 1\nend\ndrop 1\n"},
		{"copyStringsOntoItself", prelude + `func main() {
	s := []string{strings.Repeat("a", 20), strings.Repeat("b", 20), strings.Repeat("c", 20)}
	copy(s, s)
	copy(s[1:], s)
	copy(s, s[1:])
	println(s[0], s[1], s[2])
}
`, "aaaaaaaaaaaaaaaaaaaa bbbbbbbbbbbbbbbbbbbb bbbbbbbbbbbbbbbbbbbb\n"},
		// The caller's count stays with the caller: the parameter's new
		// value is the callee's own, released when the callee returns.
		{"borrowedParameter", prelude + `func g(r shared[Res]) {
	println("g", r.id)
	r = mk(9)
	println("g", r.id)
}
func gs(s string) {
	s = strings.Repeat("y", 20)
	println(s)
}
func main() {
	a := mk(1)
	g(a)
	println("after", a.id)
	g(mk(5))
	s := strings.Repeat("x", 20)
	gs(s)
	println(s)
}
`, "g 1\ng 9\ndrop 9\nafter 1\ng 5\ng 9\ndrop 9\ndrop 5\nyyyyyyyyyyyyyyyyyyyy\nxxxxxxxxxxxxxxxxxxxx\ndrop 1\n"},
		// The twin: a parameter that is only read, and a copy between
		// slices that do not overlap, count as before.
		{"ordinaryCode", prelude + `func show(r shared[Res]) { println("show", r.id) }
func main() {
	a := mk(1)
	show(a)
	src := [2]shared[Res]{mk(2), mk(3)}
	var dst [2]shared[Res]
	copy(dst[:], src[:])
	println(dst[0].id, dst[1].id, src[0].id)
	println("end")
}
`, "show 1\n2 3 2\nend\n"},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				exe := buildDebugAlloc(t, dropUnusedStrings(c.source), opt)
				out, status := runDebugAlloc(t, exe, "")
				if c.name == "ordinaryCode" {
					// The order of the drops at the end of main is the
					// order of the variables; only the count matters here.
					if status != 0 || len(out) < len(c.output) || out[:len(c.output)] != c.output {
						t.Fatalf("status %d, output %q; want status 0 and the prefix %q", status, out, c.output)
					}
					return
				}
				if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}

// dropUnusedStrings removes the pluto/strings import of a test prelude from
// a program that does not use the package.
func dropUnusedStrings(src string) string {
	if strings.Count(src, "strings.") > 0 {
		return src
	}
	return strings.Replace(src, `; "pluto/strings")`, ")", 1)
}
