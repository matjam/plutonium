package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Section 18.9, Live allocations (D45, D70; roadmap items 152 and 198): a
// Fixed or Chunked arena counts every allocation made through its Alloc
// and not freed yet, and Reset, Release and Drop panic with "arena has live
// allocations" while the count is not zero. The plain views of New and
// Slice are not counted.
func TestArenaLiveAllocations(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, source, output string
		panic                bool
	}{
		// Audit B1: a counted string kept past the Reset.
		{"stringKeptPastReset", `package main
import ("pluto/alloc"; "pluto/arena"; "pluto/fmt")
type Server owner struct { arena arena.Chunked; last string }
func (s *Server) step(n int) {
	r := alloc.Use(&s.arena)
	s.last = fmt.Sprintf("item %d", n)
	r.Restore()
	s.arena.Reset()
}
var s = Server{arena: arena.NewChunked()}
func main() { s.step(1); println("unreachable", s.last) }
`, "arena has live allocations", true},
		// Attack-mem finding 15: a string made in an alloc.Use window of a
		// local arena and still referenced at the Reset. The count stops the
		// Reset before the string is used (section 18.9, Live allocations:
		// "a Reset or Release that comes before the program has dropped what
		// it allocated").
		{"localWindowStringKeptPastReset", `package main
import ("pluto/alloc"; "pluto/arena"; "pluto/strconv")
func main() {
	a := arena.NewFixed(4096)
	r := alloc.Use(&a)
	s := strconv.Itoa(123456)
	r.Restore()
	a.Reset()
	println("unreachable", s)
}
`, "arena has live allocations", true},
		// Audit E3: an owner made in the arena and moved away.
		{"ownerMovedAway", `package main
import "pluto/arena"
type Job struct { n int }
var a = arena.NewChunked()
func main() {
	ch := make(chan owned[Job], 1)
	j := new(Job, &a)
	ch <- j
	a.Reset()
	println("unreachable", len(ch))
}
`, "arena has live allocations", true},
		{"fixedReset", `package main
import "pluto/arena"
type Job struct { n int }
var a = arena.NewFixed(4096)
func main() { j := new(Job, &a); a.Reset(); println("unreachable", j.n) }
`, "arena has live allocations", true},
		{"fixedDrop", `package main
import "pluto/arena"
func main() {
	a := arena.NewFixed(4096)
	_ = a.Alloc(16, 8)
	a.Drop()
	println("unreachable")
}
`, "arena has live allocations", true},
		{"chunkedDrop", `package main
import "pluto/arena"
func main() {
	a := arena.NewChunked()
	_ = a.Alloc(16, 8)
	a.Drop()
	println("unreachable")
}
`, "arena has live allocations", true},
		// Release checks the whole count: an allocation made before the
		// mark stops it too.
		{"releaseBeforeMark", `package main
import "pluto/arena"
func main() {
	a := arena.NewChunked()
	_ = a.Alloc(16, 8)
	m := a.Mark()
	_ = a.Slice[byte](8)
	a.Release(m)
	println("unreachable")
}
`, "arena has live allocations", true},
		{"fixedReleaseBeforeMark", `package main
import "pluto/arena"
func main() {
	a := arena.NewFixed(4096)
	_ = a.Alloc(16, 8)
	m := a.Mark()
	a.Release(m)
	println("unreachable")
}
`, "arena has live allocations", true},
		// The twins: what is freed, and the plain views, do not stop the
		// arena.
		{"freedThenReset", `package main
import "pluto/arena"
type Job struct { n int }
var a = arena.NewChunked()
var f = arena.NewFixed(4096)
func main() {
	j := new(Job, &a)
	j.n = 3
	k := new(Job, &f)
	k.n = 4
	println(j.n, k.n)
	j.Drop()
	k.Drop()
	p := a.Alloc(16, 8)
	a.Free(p, 16, 8)
	q := f.Alloc(16, 8)
	f.Free(q, 16, 8)
	a.Reset()
	f.Reset()
	println("reset")
}
`, "3 4\nreset\n", false},
		{"viewsUncounted", `package main
import "pluto/arena"
func main() {
	a := arena.NewChunked()
	f := arena.NewFixed(4096)
	m := a.Mark()
	fm := f.Mark()
	p := a.New[int](); *p = 1
	s := a.Slice[int](4); s[3] = 2
	q := f.New[int](); *q = 3
	t := f.Slice[int](4); t[3] = 4
	println(*p, s[3], *q, t[3])
	a.Release(m)
	f.Release(fm)
	_ = a.New[int]()
	_ = f.New[int]()
	a.Reset()
	f.Reset()
	println("reset")
	a.Drop()
	f.Drop()
}
`, "1 2 3 4\nreset\n", false},
		{"stringReleasedFirst", `package main
import ("pluto/alloc"; "pluto/arena"; "pluto/fmt")
type Server owner struct { arena arena.Chunked; last string }
func (s *Server) step(n int) int {
	r := alloc.Use(&s.arena)
	s.last = fmt.Sprintf("item %d", n)
	r.Restore()
	k := len(s.last)
	s.last = ""
	s.arena.Reset()
	return k
}
var s = Server{arena: arena.NewChunked()}
func main() { println(s.step(1), s.step(22)) }
`, "6 7\n", false},
		// An Alloc that fails leaves the count as it was.
		{"failedAllocUncounted", `package main
import "pluto/arena"
func main() {
	a := arena.NewFixed(64)
	p := a.Alloc(4096, 8)
	println(p == nil)
	a.Reset()
	a.Drop()
	println("dropped")
}
`, "true\ndropped\n", false},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				out, status := runSrc(t, c.source, opt)
				if c.panic {
					if status != 2 || !strings.HasPrefix(out, "panic: "+c.output+"\n") || strings.Contains(out, "unreachable") {
						t.Fatalf("status %d, output %q; want the panic %q", status, out, c.output)
					}
				} else if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}
