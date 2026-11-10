package tests

import (
	"fmt"
	"strings"
	"testing"
)

// Section 18.9, Drop entries (D74; roadmap item 202): New and Slice of a
// Fixed or Chunked arena record a drop entry for a type with drop glue, and
// Reset, Release and Drop run the entries, last first, before they check
// the count of live allocations.
func TestArenaDropEntries(t *testing.T) {
	t.Parallel()
	const res = `type Res owner struct { id int }
func (r *Res) Drop() { if r.id != 0 { println("drop", r.id) } }
`
	cases := []struct {
		name, source, output string
		panic                string // the panic message the output ends with
	}{
		{"reverseOrder", `package main
import "pluto/arena"
` + res + `func main() {
	a := arena.NewChunked()
	a.New[Res]().id = 1
	s := a.Slice[Res](2)
	s[0].id = 2
	s[1].id = 3
	a.New[Res]().id = 4
	a.Reset()
	println("reset")
	f := arena.NewFixed(4096)
	f.New[Res]().id = 5
	t := f.Slice[Res](2)
	t[0].id = 6
	t[1].id = 7
	f.Reset()
	println("reset")
}
`, "drop 4\ndrop 3\ndrop 2\ndrop 1\nreset\ndrop 7\ndrop 6\ndrop 5\nreset\n", ""},
		{"dropRunsEntries", `package main
import "pluto/arena"
` + res + `func main() {
	a := arena.NewChunked()
	a.New[Res]().id = 1
	a.Drop()
	f := arena.NewFixed(4096)
	f.New[Res]().id = 2
	f.Drop()
	println("dropped")
}
`, "drop 1\ndrop 2\ndropped\n", ""},
		{"releaseRunsLaterEntries", `package main
import "pluto/arena"
` + res + `func main() {
	a := arena.NewChunked()
	f := arena.NewFixed(4096)
	a.New[Res]().id = 1
	f.New[Res]().id = 2
	m := a.Mark()
	fm := f.Mark()
	for i := 3; i < 40; i++ {
		a.New[Res]().id = i
	}
	f.New[Res]().id = 40
	a.Release(m)
	f.Release(fm)
	println("released")
	a.Reset()
	f.Reset()
}
`, func() string {
			var b strings.Builder
			for i := 39; i >= 3; i-- {
				fmt.Fprintf(&b, "drop %d\n", i)
			}
			return b.String() + "drop 40\nreleased\ndrop 1\ndrop 2\n"
		}(), ""},
		// Audit F1: an owner made in the arena and kept in a view is
		// dropped by the view's entry, so the count is zero by the check.
		{"ownerInView", `package main
import "pluto/arena"
type Job struct { n int }
type Holder owner struct { j owned[Job] }
var a = arena.NewChunked()
var f = arena.NewFixed(4096)
func main() {
	h := a.New[Holder]()
	h.j = new(Job, &a)
	h.j.n = 5
	k := f.Slice[Holder](2)
	k[1].j = new(Job, &f)
	k[1].j.n = 6
	println(h.j.n, k[1].j.n)
	a.Reset()
	f.Reset()
	println("reset")
}
`, "5 6\nreset\n", ""},
		{"stringsInView", `package main
import ("pluto/arena"; "pluto/strconv")
func main() {
	a := arena.NewChunked()
	s := a.Slice[string](3)
	for i := range s {
		s[i] = "item " + strconv.Itoa(i)
	}
	println(s[2])
	a.Reset()
	println("reset")
}
`, "item 2\nreset\n", ""},
		// An entry recorded while the entries run is not run, and stops the
		// call.
		{"entryRecordedWhileRunning", `package main
import "pluto/arena"
var a = arena.NewChunked()
type Res owner struct { id int }
func (r *Res) Drop() {
	if r.id == 1 {
		a.New[Res]().id = 2
	}
}
func main() {
	a.New[Res]().id = 1
	a.Reset()
	println("unreachable")
}
`, "", "arena has live allocations"},
		{"fixedEntryRecordedWhileRunning", `package main
import "pluto/arena"
var a = arena.NewFixed(4096)
type Res owner struct { id int }
func (r *Res) Drop() {
	if r.id == 1 {
		a.New[Res]().id = 2
	}
}
func main() {
	a.New[Res]().id = 1
	a.Reset()
	println("unreachable")
}
`, "", "arena has live allocations"},
		// An allocation made and freed within the entries leaves nothing.
		{"allocFreedWhileRunning", `package main
import "pluto/arena"
var a = arena.NewChunked()
type Res owner struct { id int }
func (r *Res) Drop() {
	if r.id == 1 {
		p := a.Alloc(16, 8)
		a.Free(p, 16, 8)
	}
}
func main() {
	a.New[Res]().id = 1
	a.Reset()
	println("reset")
}
`, "reset\n", ""},
		// A counted allocation still live after the entries stops the call.
		{"liveAfterEntries", `package main
import "pluto/arena"
` + res + `func main() {
	a := arena.NewChunked()
	a.New[Res]().id = 1
	_ = a.Alloc(16, 8)
	a.Reset()
	println("unreachable")
}
`, "drop 1\n", "arena has live allocations"},
		// A drop that panics leaves the entries not yet run recorded, and a
		// deferred Reset starts again from them.
		{"panicInEntry", `package main
import "pluto/arena"
var a = arena.NewChunked()
type Res owner struct { id int }
func (r *Res) Drop() {
	if r.id == 0 {
		return
	}
	println("drop", r.id)
	if r.id == 2 {
		panic("drop failed")
	}
}
func main() {
	defer a.Reset()
	for i := 1; i <= 3; i++ {
		a.New[Res]().id = i
	}
	a.Reset()
	println("unreachable")
}
`, "drop 3\ndrop 2\ndrop 1\n", "drop failed"},
		// A Fixed keeps its entries in the table in its backing: a New
		// fails when the table has no room for its entry, here the third
		// in a payload of two granules.
		{"fixedTableFull", `package main
import "pluto/arena"
` + res + `func main() {
	f := arena.NewFixed(32)
	f.New[Res]().id = 1
	println(1)
	f.New[Res]().id = 2
	println(2)
	f.New[Res]().id = 3
	println("unreachable")
}
`, "1\n2\n", "arena: out of memory"},
		// The twins: a type without drop glue records nothing, so it has
		// the whole capacity, and a Slice of length zero records nothing.
		{"plainUsesWholeCapacity", `package main
import "pluto/arena"
` + res + `func main() {
	f := arena.NewFixed(64)
	b := f.Slice[byte](64)
	println(len(b), f.Available())
	f.Reset()
	g := arena.NewFixed(32)
	_ = g.Slice[Res](0)
	g.New[Res]().id = 1
	g.Reset()
	println("reset")
}
`, "64 0\ndrop 1\nreset\n", ""},
		{"unsafeChunkedRecordsNothing", `package main
import "pluto/arena"
` + res + `func main() {
	a := arena.NewChunkedUnsafe()
	a.New[Res]().id = 1
	a.Reset()
	println("reset")
}
`, "reset\n", ""},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				out, status := runSrc(t, c.source, opt)
				if c.panic != "" {
					if status != 2 || !strings.HasPrefix(out, c.output+"panic: "+c.panic+"\n") || strings.Contains(out, "unreachable") {
						t.Fatalf("status %d, output %q; want %q and the panic %q", status, out, c.output, c.panic)
					}
				} else if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}
