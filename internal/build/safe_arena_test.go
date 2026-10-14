package build_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// Sections 18.9 and 18.11: ordinary fixed views have the same reclaimed-memory
// contract as Chunked, while a partial release preserves live prefix storage.
func TestSafeArenaMinimal(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct {
		name, source, output string
		panic                bool
	}{
		{"fixedReset", `package main
import "pluto/arena"
var a = arena.NewFixed(4096)
type Holder struct { p *int }
var held = new(Holder)
func retain() { p:=a.New[int](); *p=7; held.p=p }
func main() { retain(); a.Reset(); p:=a.New[int](); *p=99; println(*held.p) }
`, "use of memory from an arena", true},
		{"fixedLivePrefix", `package main
import "pluto/arena"
func main() {
 a:=arena.NewFixed(4096)
 p:=a.New[int](); *p=7
 m:=a.Mark()
 q:=a.New[int](); *q=8
 println(*p,*q)
 a.Release(m)
 r:=a.New[int](); *r=9
 println(*p,*r)
 a.Reset()
 fresh:=a.New[int](); *fresh=11
 println(*fresh)
}
`, "7 8\n7 9\n11\n", false},
		{"fixedByteSiblings", `package main
import "pluto/arena"
func main(){
 a:=arena.NewFixed(64)
 p:=a.New[byte]();*p=7
 m:=a.Mark()
 q:=a.New[byte]();*q=8
 a.Release(m)
 r:=a.New[byte]();*r=9
 println(*p,*r,len(a.Slice[byte](0)))
 empty:=arena.NewFixed(0)
 println(empty.Cap(),len(empty.Slice[byte](0)))
}
`, "7 9 0\n0 0\n", false},
	}
	for _, c := range cases {
		for _, opt := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, opt), func(t *testing.T) {
				testguard.Parallel(t)
				out, status := runGenCheck(t, c.source, opt)
				if c.panic {
					if status != 2 || !strings.HasPrefix(out, "panic: runtime error: "+c.output) || strings.Contains(out, "99\n") {
						t.Fatalf("status %d, output %q; want checked stale-view panic", status, out)
					}
				} else if status != 0 || out != c.output {
					t.Fatalf("status %d, output %q; want status 0 and %q", status, out, c.output)
				}
			})
		}
	}
}

// The old hosted modulo epoch must never make a saved view valid again. A
// conforming finite encoding may exhaust first; retirement/widening may instead
// permit the fresh allocations and reject the final stale access.
func TestChunkedIncarnationExhaustion(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import "pluto/arena"
var a = arena.NewChunked()
type Holder struct { p *int }
var held = new(Holder)
func retain() { p:=a.New[int](); *p=7; held.p=p }
func main() {
 retain()
 for i:=0; i<32768; i++ { a.Reset(); p:=a.New[int](); *p=i }
 println("matched",*held.p)
}


`
	for _, opt := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runGenCheck(t, source, opt)
			if status != 2 || !strings.HasPrefix(out, "panic: ") || strings.Contains(out, "matched ") {
				t.Fatalf("status %d, output %q; want loud exhaustion or stale-view panic", status, out)
			}
			if !strings.Contains(out, "out of memory") && !strings.Contains(out, "incarnation exhausted") && !strings.Contains(out, "use of memory from an arena") {
				t.Fatalf("unrelated panic is not an exhaustion/lifetime receipt: %q", out)
			}
		})
	}
}

// Explicit parent accounting distinguishes payload from per-allocation
// records, and checks that Release/Reset/Drop return every owned allocation.
func TestFixedParentAllocationCost(t *testing.T) {
	testguard.Parallel(t)
	const source = `package main
import ("pluto/alloc"; "pluto/arena"; "unsafe")
type counter struct { base alloc.Allocator; live, allocations, frees int }
func (c *counter) Alloc(size,align uintptr) unsafe.Pointer {
 p:=c.base.Alloc(size,align)
 if p!=nil { c.live++; c.allocations++ }
 return p
}
func (c *counter) Free(p unsafe.Pointer,size,align uintptr) {
 if p!=nil { c.live--; c.frees++ }
 c.base.Free(p,size,align)
}
func exercise(c *counter) {
 a:=arena.NewFixed(128,c)
 println("payload",c.live)
 p:=a.New[int](); *p=7
 m:=a.Mark()
 q:=a.New[int](); *q=8
 println("records",c.live,*p,*q)
 a.Release(m)
 println("prefix",c.live,*p)
 a.Reset()
 println("reset",c.live)
}
func main() {
 c:=counter{base:alloc.Default()}
 exercise(&c)
 println("done",c.live,c.allocations,c.frees)
}
`
	const want = "payload 1\nrecords 3 7 8\nprefix 2 7\nreset 1\ndone 0 3 3\n"
	for _, opt := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", opt), func(t *testing.T) {
			testguard.Parallel(t)
			out, status := runGenCheck(t, source, opt)
			if status != 0 || out != want {
				t.Fatalf("status %d, output %q; want status0 and %q", status, out, want)
			}
		})
	}
}
