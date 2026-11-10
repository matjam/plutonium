package tests

import (
	"fmt"
	"strings"
	"testing"
)

// abi.md section 1.3 and D54 (roadmap item 153): a Fixed arena takes its
// bookkeeping table from its own backing, in the one request to its parent
// when it is made, and never asks the parent again. An allocation that
// finds no room in the table fails without advancing the cursor.
func TestArenaFixedTable(t *testing.T) {
	t.Parallel()
	const counting = `type counting struct { under alloc.Allocator; allocs, frees int }
func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.allocs++
	return c.under.Alloc(size, align)
}
func (c *counting) Free(p unsafe.Pointer, size, align uintptr) {
	c.frees++
	c.under.Free(p, size, align)
}
type Res owner struct { id int }
func (r *Res) Drop() { if r.id != 0 { println("drop", r.id) } }
`
	cases := []struct {
		name, source, output string
		panic                string // the panic message the output ends with
	}{
		// Allocation, Mark, Release, Reset and the drop entries call the
		// parent for nothing: one request when the arena is made, one free
		// when it is dropped.
		{"oneParentRequest", `package main
import ("pluto/alloc"; "pluto/arena"; "unsafe")
` + counting + `func main() {
	c := &counting{under: alloc.Default()}
	f := arena.NewFixed(1024, c)
	println("made", c.allocs, c.frees)
	for i := 0; i < 3; i++ {
		m := f.Mark()
		for j := 0; j < 8; j++ {
			f.New[Res]().id = i*10 + j + 1
			_ = f.Slice[int](3)
			p := f.Alloc(24, 8)
			f.Free(p, 24, 8)
		}
		f.Release(m)
		f.New[int]()
		f.Reset()
	}
	println("used", c.allocs, c.frees)
	f.Drop()
	println("dropped", c.allocs, c.frees)
}
`, "", ""},
		// The table has room for one allocation and its drop entry per
		// granule: two 8-byte values fit the 16-byte payload, but the
		// table has room for one entry. The failed New leaves the cursor
		// where it was. Allocations of less than a granule fill the
		// table before the payload.
		{"tableFull", `package main
import "pluto/arena"
type Res owner struct { id int }
func (r *Res) Drop() { if r.id != 0 { println("drop", r.id) } }
func main() {
	f := arena.NewFixed(16)
	f.New[Res]().id = 1
	println(f.Used())
	f.New[Res]().id = 2
	println("unreachable")
}
`, "8\n", "arena: out of memory"},
		{"tableFullAlloc", `package main
import "pluto/arena"
func main() {
	f := arena.NewFixed(16)
	p := f.Alloc(1, 1)
	q := f.Alloc(1, 1)
	println(p != nil && q != nil, f.Used())
	r := f.Alloc(1, 1)
	println(r == nil, f.Used())
	f.Free(q, 1, 1)
	f.Free(p, 1, 1)
	f.Drop()
}
`, "true 2\ntrue 2\n", ""},
		// The twin: allocations of whole granules fill the payload before
		// the table, and each New of a type with drop glue has its entry.
		{"wholeGranulesFillPayload", `package main
import "pluto/arena"
type Res owner struct { id int; pad int }
func (r *Res) Drop() { if r.id != 0 { println("drop", r.id) } }
func main() {
	f := arena.NewFixed(64)
	for i := 1; i <= 4; i++ {
		f.New[Res]().id = i
	}
	println(f.Used(), f.Available())
	f.Reset()
	b := f.Slice[byte](64)
	println(len(b), f.Available())
	f.Drop()
}
`, "64 0\ndrop 4\ndrop 3\ndrop 2\ndrop 1\n64 0\n", ""},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				t.Parallel()
				out, status := runSrc(t, c.source, opt)
				want := c.output
				if c.name == "oneParentRequest" {
					want = "drop 8\ndrop 7\ndrop 6\ndrop 5\ndrop 4\ndrop 3\ndrop 2\ndrop 1\n"
					want += strings.ReplaceAll(want, "drop ", "drop 1")
					want += "drop 28\ndrop 27\ndrop 26\ndrop 25\ndrop 24\ndrop 23\ndrop 22\ndrop 21\n"
					want = "made 1 0\n" + want + "used 1 0\ndropped 1 1\n"
				}
				if c.panic != "" {
					if status != 2 || !strings.HasPrefix(out, want+"panic: "+c.panic+"\n") || strings.Contains(out, "unreachable") {
						t.Fatalf("status %d, output %q; want %q and the panic %q", status, out, want, c.panic)
					}
				} else if status != 0 || out != want {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, want)
				}
			})
		}
	}
}
