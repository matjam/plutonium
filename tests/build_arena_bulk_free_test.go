package tests

import (
	"fmt"
	"testing"
)

// Section 18.9, Allocators, and F.2 (D46; roadmap item 154): an arena takes
// memory back in bulk only. Free through an arena takes back nothing, not
// even the latest allocation; Reset, Release and Drop take memory back.
func TestArenaBulkFree(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, source, output string }{
		{"chunkedFreeLatest", `package main
import "pluto/arena"
func main() {
	a := arena.NewChunked()
	p := a.Alloc(100, 8)
	u := a.Used()
	a.Free(p, 100, 8)
	println(a.Used() == u)
	q := a.Alloc(8, 8)
	println(q != p, a.Used() > u)
	a.Free(q, 8, 8)
	a.Drop()
}
`, "true\ntrue true\n"},
		{"fixedFreeLatest", `package main
import "pluto/arena"
func main() {
	a := arena.NewFixed(256)
	p := a.Alloc(100, 8)
	u := a.Used()
	a.Free(p, 100, 8)
	println(a.Used() == u)
	q := a.Alloc(8, 8)
	println(q != p, a.Used() > u)
	a.Free(q, 8, 8)
	a.Drop()
}
`, "true\ntrue true\n"},
		{"unsafeArenaFreeLatest", `package main
import ("pluto/arena"; "unsafe")
func main() {
	var mem [256]uint64
	a := arena.NewUnsafe(unsafe.Slice((*byte)(unsafe.Pointer(&mem)), len(mem)*8))
	p := a.Alloc(100, 8)
	u := a.Used()
	a.Free(p, 100, 8)
	println(a.Used() == u)
	q := a.Alloc(8, 8)
	println(q != p, a.Used() > u)
}
`, "true\ntrue true\n"},
		// A partial Free of the latest allocation takes back nothing either.
		{"partialFree", `package main
import ("pluto/arena"; "unsafe")
func main() {
	a := arena.NewChunked()
	p := a.Alloc(64, 8)
	u := a.Used()
	a.Free(unsafe.Add(p, 32), 32, 8)
	println(a.Used() == u)
	a.Drop()
}
`, "true\n"},
		// The twin: Reset, Release and Drop take memory back as before.
		{"bulkStillReclaims", `package main
import "pluto/arena"
func main() {
	a := arena.NewChunked()
	f := arena.NewFixed(256)
	m, n := a.Mark(), f.Mark()
	p, q := a.Alloc(100, 8), f.Alloc(100, 8)
	a.Free(p, 100, 8)
	f.Free(q, 100, 8)
	a.Release(m)
	f.Release(n)
	println(a.Used(), f.Used())
}
`, "0 0\n"},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				out, status := runSrc(t, c.source, opt)
				if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}
