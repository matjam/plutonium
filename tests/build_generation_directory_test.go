// Ported from internal/build/generation_directory_test.go at the go-cutover tag.

package tests

import (
	"fmt"
	"strings"
	"testing"
)

// TestGenerationDirectoryLarge checks interior addresses beyond the first
// 64 KiB interval and reads after both heap and arena reclamation.
func TestGenerationDirectoryLarge(t *testing.T) {
	t.Parallel()
	for _, allocator := range []string{"heap", "arena"} {
		for _, stale := range []bool{false, true} {
			for _, level := range []int{0, 2} {
				t.Run(fmt.Sprintf("%s/stale=%v/O%d", allocator, stale, level), func(t *testing.T) {
					t.Parallel()
					setup, reclaim := "a := alloc.Default()", "a.Free(p, 200000, 16)"
					if allocator == "arena" {
						setup, reclaim = "a := arena.NewChunked(); defer a.Drop()", "a.Reset()"
					}
					if !stale {
						reclaim = ""
					}
					src := `package main
import ("pluto/alloc"; "pluto/arena"; "unsafe")
var _ alloc.Allocator
var _ arena.Chunked
var _ unsafe.Pointer
func main() {
` + setup + `
p := a.Alloc(200000, 16)
b := unsafe.Slice((*byte)(p), 200000)
b[150001] = 73
` + reclaim + `
println(b[150001])
}`
					out, status := runGenCheck(t, src, level)
					if stale {
						if status != 2 || !strings.Contains(out, "use of") {
							t.Fatalf("expected stale access panic: status %d, output %q", status, out)
						}
					} else if status != 0 || out != "73\n" {
						t.Fatalf("live allocation: status %d, output %q", status, out)
					}
				})
			}
		}
	}
}

func TestGenerationDirectoryComposition(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, decl, body, want string
		status                 int
	}{
		{"same granule short bounds", customGenerationAllocator, `
var a Tiny
p := a.Alloc(1, 1)
println(*(*uint64)(p))`, "use of freed memory", 2},
		{"custom direct reuse", customGenerationAllocator, `
var a Tiny
p := a.Alloc(4, 1)
b := (*byte)(p)
a.Free(p, 4, 1)
q := a.Alloc(200000, 1)
*(*byte)(q) = 81
println(*b)`, "use of freed memory", 2},
		{"custom generation retirement", customGenerationAllocator, `
var a Tiny
first := a.Alloc(1, 1)
a.Free(first, 1, 1)
for i := 1; i < 32767; i++ {
 p := a.Alloc(1, 1)
 if p == nil { panic("early retirement") }
 a.Free(p, 1, 1)
}
for i := 0; i < 20; i++ {
 a.off = 0
 if a.Alloc(uintptr(i+1), 1) != nil { panic("generation restarted") }
}
println("retired")
println(*(*byte)(first))`, "retired\npanic: runtime error: use of freed memory", 2},
		{"bodyless allocator", `
var storage [200000]byte
type External struct {}
var external External
//pu:extern gap10_alloc
//pu:noescape
func (a *External) Alloc(size, align uintptr) unsafe.Pointer
//pu:extern gap10_free
//pu:noescape
func (a *External) Free(p unsafe.Pointer, size, align uintptr)
//pu:export gap10_alloc
func rawAlloc(a *External, size, align uintptr) unsafe.Pointer { return unsafe.Pointer(&storage[0]) }
//pu:export gap10_free
func rawFree(a *External, p unsafe.Pointer, size, align uintptr) {}
`, `
a := &external
get := a.Alloc
p := get(200000, 1)
*(*byte)(unsafe.Add(p, 150001)) = 35
println(*(*byte)(unsafe.Add(p, 150001)))
var iface alloc.Allocator = a
iface.Free(p, 200000, 1)
println(*(*byte)(unsafe.Add(p, 150001)))`, "35\npanic: runtime error: use of freed memory", 2},
		{"custom method value", customGenerationAllocator, `
var a Tiny
get, free := a.Alloc, a.Free
p := get(200000, 1)
*(*byte)(p) = 37
println(*(*byte)(p))
free(p, 200000, 1)
println(*(*byte)(p))`, "37\npanic: runtime error: use of freed memory", 2},
		{"large double free", "", `
a := alloc.Default()
p := a.Alloc(200000, 65536)
println(uintptr(p)%65536)
a.Free(p, 200000, 65536)
a.Free(p, 200000, 65536)`, "0\npanic: alloc: Free of memory that was already freed", 2},
		{"large arena release", "", `
a := arena.NewChunked(200000)
ra := &a; _ = ra
m := a.Mark()
p := (*byte)(a.Alloc(150000, 65536))
*p = 33
println(uintptr(unsafe.Pointer(p))%65536)
ra.Release(m)
println(*p)`, "0\npanic: runtime error: use of memory from an arena", 2},
		{"partial tail indexed access", "", `
a := arena.NewChunked()
p := a.Alloc(200000, 16)
b := unsafe.Slice((*byte)(p), 200000)
b[0] = 41
a.Free(unsafe.Add(p, 100000), 100000, 16)
println(b[0])
println(b[150001])`, "41\npanic: runtime error: use of freed memory", 2},
		{"partial tail crossing load", "", `
a := arena.NewChunked()
p := a.Alloc(200000, 16)
a.Free(unsafe.Add(p, 100000), 100000, 16)
println(*(*uint16)(unsafe.Add(p, 99999)))`, "use of freed memory", 2},
		{"partial tail bulk copy", "", `
a := arena.NewChunked()
p := a.Alloc(200000, 16)
b := unsafe.Slice((*uint64)(p), 25000)
a.Free(unsafe.Add(p, 100000), 100000, 16)
var dst [25000]uint64
copy(dst[:], b)`, "use of freed memory", 2},
		{"copy drop reclaims destination", `
var outer arena.Chunked
type Retirer owner struct{}
func (r *Retirer) Drop() { outer.Reset() }
`, `
outer = arena.NewChunked()
dst := alloc.New[[1]shared[Retirer]](&outer)[:]
dst[0] = alloc.Shared[Retirer]()
var src [1]shared[Retirer]
copy(dst, src[:])`, "use of memory from an arena", 2},
		{"large arena drop", `
// A package variable, not a local one: a local arena's allocations are
// views of it (spec 18.3), and their use after Drop does not compile.
var a arena.Chunked
`, `
a = arena.NewChunked(200000)
p := (*byte)(a.Alloc(150000, 16))
*p = 34
dead := take(&a)
dead.Drop()
println(*p)`, "use of memory from an arena", 2},
		{"large counted string", "", `
a := arena.NewChunked()
defer alloc.Use(&a).Restore()
s := strings.Repeat("x", 200000)
println(len(s), s[150001])
a.Reset()
c := s
println(c[150001])`, "200000 120\npanic: runtime error: use of memory from an arena", 2},
		{"large counted box", `type Huge struct { bytes [200000]byte }`, `
a := arena.NewChunked()
defer alloc.Use(&a).Restore()
b := alloc.Shared[Huge]()
b.bytes[150001] = 36
println(b.bytes[150001])
a.Reset()
c := b
println(c.bytes[150001])`, "36\npanic: runtime error: use of memory from an arena", 2},
		{"zero counted custom", customGenerationAllocator, `
var a Tiny
defer alloc.Use(&a).Restore()
b := alloc.Shared[struct{}]()
c := b
w := weak[shared[struct{}]](b)
p, ok := w.Upgrade()
println(c != nil, p != nil, ok)`, "true true true\n", 0},
		{"large counted interface", `
type HugeError struct { bytes [200000]byte }
func (e *HugeError) Error() string { return "huge" }
`, `
a := arena.NewChunked()
defer alloc.Use(&a).Restore()
var e error = alloc.Synced[HugeError]()
println(e.Error())
a.Reset()
c := e
println(c.Error())`, "huge\npanic: runtime error: use of memory from an arena", 2},
		{"stale runtime map", "", `
a := arena.NewChunked()
defer alloc.Use(&a).Restore()
m := make(map[int][200000]byte)
m[1] = [200000]byte{}
a.Reset()
println(len(m))`, "use of memory from an arena", 2},
		{"stale runtime channel", "", `
a := arena.NewChunked()
defer alloc.Use(&a).Restore()
c := make(chan [200000]byte, 1)
c <- [200000]byte{}
a.Reset()
println(len(c))`, "use of memory from an arena", 2},
		{"zero synced arena", `
func zeroSynced(a *arena.Chunked) {
defer alloc.Use(a).Restore()
b := alloc.Synced[struct{}]()
c := b
w := weak[synced[struct{}]](b)
p, ok := w.Upgrade()
println(c != nil, p != nil, ok)
}`, `
a := arena.NewChunked()
defer a.Drop()
zeroSynced(&a)`, "true true true\n", 0},
		{"custom constructor", customGenerationAllocator, `
var a Tiny
p := alloc.New[[200000]byte](&a)
p[150001] = 45
println(p[150001])
alloc.Free(p, &a)
println(p[150001])`, "45\npanic: runtime error: use of freed memory", 2},
		{"custom scoped override", customGenerationAllocator, `
var a Tiny
defer alloc.Use(&a).Restore()
p := alloc.New[[200000]byte]()
p[150001] = 46
println(p[150001])
alloc.Free(p)
println(p[150001])`, "46\npanic: runtime error: use of freed memory", 2},
		{"byte aligned siblings", customGenerationAllocator, `
var a Tiny
p := a.Alloc(1, 1)
q := a.Alloc(1, 1)
*(*byte)(p) = 17
*(*byte)(q) = 18
println(*(*byte)(p), *(*byte)(q))
a.Free(q, 1, 1)
println(*(*byte)(p))`, "17 18\n17\n", 0},
		{"parent reset", "", `
outer := arena.NewChunked()
ro := &outer; _ = ro
inner := arena.NewChunked(256, &outer)
p := (*byte)(inner.Alloc(200000, 16))
*p = 19
ro.Reset()
println(*p)`, "use of memory from an arena", 2},
		{"nonforwarding free byte aligned", customGenerationAllocator + `
type Wrapped struct { alloc.Allocator }
func (a *Wrapped) Free(p unsafe.Pointer, size, align uintptr) {}
`, `
var tiny Tiny
a := Wrapped{&tiny}
p := a.Alloc(1, 1)
q := a.Alloc(1, 1)
*(*byte)(p) = 31
*(*byte)(q) = 32
a.Free(q, 1, 1)
println(*(*byte)(p))
println(*(*byte)(q))`, "31\npanic: runtime error: use of freed memory", 2},
		{"partial parent release", "", `
outer := arena.NewChunked()
ro := &outer; _ = ro
left := arena.NewChunked(256, &outer)
p := (*byte)(left.Alloc(1, 16))
*p = 21
m := outer.Mark()
right := arena.NewChunked(256, &outer)
q := (*byte)(right.Alloc(1, 16))
*q = 22
ro.Release(m)
println(*p)
println(*q)`, "21\npanic: runtime error: use of memory from an arena", 2},
		{"custom free promoted alloc", `
type Wrapped struct { *alloc.SyncSlab }
func (a *Wrapped) Free(p unsafe.Pointer, size, align uintptr) {
 a.SyncSlab.Free(p, size, align)
}`, `
a := Wrapped{alloc.NewSyncSlab(alloc.Pages)}
p := a.Alloc(200000, 16)
*(*byte)(p) = 24
println(*(*byte)(p))
a.Free(p, 200000, 16)`, "24\n", 0},
		{"custom alloc promoted free", `
type Wrapped struct { *alloc.SyncSlab }
func (a *Wrapped) Alloc(size, align uintptr) unsafe.Pointer {
 return a.SyncSlab.Alloc(size, align)
}`, `
a := Wrapped{alloc.NewSyncSlab(alloc.Pages)}
p := a.Alloc(200000, 16)
*(*byte)(p) = 25
println(*(*byte)(p))
a.Free(p, 200000, 16)`, "25\n", 0},
		{"generic custom promoted alloc", `
var storage [32]byte
type Parent[A, B any] struct{}
func (a *Parent[A, B]) Alloc(size, align uintptr) unsafe.Pointer { return unsafe.Pointer(&storage[0]) }
func (a *Parent[A, B]) Free(p unsafe.Pointer, size, align uintptr) {}
type Child[T any] struct { Parent[string, T] }
func (a *Child[T]) Free(p unsafe.Pointer, size, align uintptr) { a.Parent.Free(p, size, align) }
`, `
var a Child[int]
p := a.Alloc(16, 1)
*(*byte)(p) = 28
println(*(*byte)(p))
a.Free(p, 16, 1)
println("freed")
println(*(*byte)(p))`, "28\nfreed\npanic: runtime error: use of freed memory", 2},
		{"custom alloc promoted custom free", `
var storage [32]byte
var parentView unsafe.Pointer
type Parent[A, B any] struct{}
func (a *Parent[A, B]) Alloc(size, align uintptr) unsafe.Pointer { return unsafe.Pointer(&storage[0]) }
func (a *Parent[A, B]) Free(p unsafe.Pointer, size, align uintptr) {}
type Child[T any] struct { Parent[string, T] }
func (a *Child[T]) Alloc(size, align uintptr) unsafe.Pointer {
 p := a.Parent.Alloc(size, align)
 parentView = p
 return p
}
`, `
var a Child[int]
p := a.Alloc(16, 1)
*(*byte)(p) = 29
a.Free(p, 16, 1)
println("freed")
println(*(*byte)(parentView))`, "freed\npanic: runtime error: use of freed memory", 2},
		{"nonforwarding promoted double free", customGenerationAllocator + `
type Wrapped struct { alloc.Allocator }
func (a *Wrapped) Free(p unsafe.Pointer, size, align uintptr) {}
`, `
var tiny Tiny
a := Wrapped{&tiny}
p := a.Alloc(1, 1)
a.Free(p, 1, 1)
println("freed")
a.Free(p, 1, 1)`, "freed\npanic: alloc: Free of memory that was already freed", 2},
		{"promoted free invalid extent before body", customGenerationAllocator + `
type Wrapped struct { alloc.Allocator }
func (a *Wrapped) Free(p unsafe.Pointer, size, align uintptr) { println("entered body") }
`, `
var tiny Tiny
a := Wrapped{&tiny}
p := a.Alloc(1, 1)
a.Free(p, 8, 1)`, "panic: alloc: Free of memory that was already freed", 2},
		{"generic free promoted alloc", `
type Wrapped[T any] struct { *alloc.SyncSlab }
func (a *Wrapped[T]) Free(p unsafe.Pointer, size, align uintptr) {
 a.SyncSlab.Free(p, size, align)
}`, `
a := Wrapped[int]{alloc.NewSyncSlab(alloc.Pages)}
p := a.Alloc(200000, 16)
*(*byte)(p) = 26
println(*(*byte)(p))
a.Free(p, 200000, 16)`, "26\n", 0},
		{"free promoted interface alloc", `
type Wrapped struct { alloc.Allocator }
func (a *Wrapped) Free(p unsafe.Pointer, size, align uintptr) {
 a.Allocator.Free(p, size, align)
}`, `
a := Wrapped{alloc.Default()}
p := a.Alloc(200000, 16)
*(*byte)(p) = 27
println(*(*byte)(p))
a.Free(p, 200000, 16)`, "27\n", 0},
		{"tagged page provider", `
var outer arena.Chunked
type PageArena struct { a *arena.Chunked }
func (p PageArena) AllocPages(n uintptr) unsafe.Pointer { return p.a.Alloc(n*4096, 4096) }
func (p PageArena) FreePages(b unsafe.Pointer, n uintptr) { p.a.Free(b, n*4096, 4096) }
`, `
outer = arena.NewChunked()
a := alloc.NewSlab(PageArena{&outer})
p := a.Alloc(200000, 16)
*(*byte)(p) = 28
println(*(*byte)(p))
outer.Reset()
println(*(*byte)(p))`, "28\npanic: runtime error: use of freed memory", 2},
		{"tagged page provider small", `
var outer arena.Chunked
type PageArena struct { a *arena.Chunked }
func (p PageArena) AllocPages(n uintptr) unsafe.Pointer { return p.a.Alloc(n*4096, 4096) }
func (p PageArena) FreePages(b unsafe.Pointer, n uintptr) { p.a.Free(b, n*4096, 4096) }
`, `
outer = arena.NewChunked()
a := alloc.NewSlab(PageArena{&outer})
p := a.Alloc(64, 16)
*(*byte)(p) = 29
println(*(*byte)(p))
outer.Reset()
println(*(*byte)(p))`, "29\npanic: runtime error: use of freed memory", 2},
		{"stale slab free list", generationPageProvider, `
outer = arena.NewChunked()
a := *alloc.NewSlab(PageArena{&outer})
p := a.Alloc(64, 16)
a.Free(p, 64, 16)
outer.Reset()
a.Alloc(64, 16)`, "slab backing memory was reclaimed", 2},
		{"stale sync slab cache", generationPageProvider, `
outer = arena.NewChunked()
a := *alloc.NewSyncSlab(PageArena{&outer})
p := a.Alloc(64, 16)
a.Free(p, 64, 16)
outer.Reset()
a.Alloc(64, 16)`, "slab backing memory was reclaimed", 2},
		{"reconstructed slab identity", generationPageProvider + `
func round() {
 a := *alloc.NewSlab(PageArena{&outer})
 p := (*byte)(a.Alloc(64, 16))
 *p = 38
 if *p != 38 { panic("bad live slab") }
}
`, `
outer = arena.NewChunked()
for i := 0; i < 100; i++ { round(); outer.Reset() }
println("live")`, "live\n", 0},
		{"partial slab backing release", generationPageProvider, `
outer = arena.NewChunked()
a := *alloc.NewSlab(PageArena{&outer})
p := (*byte)(a.Alloc(64, 16))
*p = 39
m := outer.Mark()
q := (*byte)(a.Alloc(128, 16))
*q = 40
outer.Release(m)
other := *alloc.NewSlab(PageArena{&outer})
other.Alloc(128, 16)
println(*p)
println(*q)`, "39\npanic: runtime error: use of freed memory", 2},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			t.Run(fmt.Sprintf("%s/O%d", c.name, level), func(t *testing.T) {
				t.Parallel()
				src := `package main
import ("pluto/alloc"; "pluto/arena"; "pluto/strings"; "unsafe")
var _ alloc.Allocator
var _ arena.Chunked
var _ unsafe.Pointer
var _ = strings.Repeat
` + c.decl + "\nfunc main() {\n" + c.body + "\n}"
				// A case that resets or releases a local arena through ra or
				// ro reaches the run time check; on the arena itself the
				// stale use is a compile error (E3313, section 18.9).
				if strings.Contains(src, "_ = ra") || strings.Contains(src, "_ = ro") {
					direct := strings.NewReplacer("ra.Release", "a.Release", "ro.Reset", "outer.Reset", "ro.Release", "outer.Release").Replace(src)
					if out := buildFails(t, direct); !strings.Contains(out, "E3313") || !strings.Contains(out, "reclaimed by") {
						t.Fatalf("direct reclaim: output %q; want E3313", out)
					}
				}
				out, status := runGenCheck(t, src, level)
				if status != c.status || !strings.Contains(out, c.want) {
					t.Fatalf("want status %d and %q: got status %d, output %q", c.status, c.want, status, out)
				}
				if c.name == "promoted free invalid extent before body" && strings.Contains(out, "entered body") {
					t.Fatalf("invalid Free extent reached the allocator body: %q", out)
				}
			})
		}
	}
}

const customGenerationAllocator = `
var storage [1000000]byte
type Tiny struct { off uintptr }
func (a *Tiny) Alloc(size, align uintptr) unsafe.Pointer {
 if align == 0 { align = 1 }
 start := (a.off+align-1) &^ (align-1)
 if start > 1000000 || size > 1000000-start { return nil }
 p := unsafe.Add(unsafe.Pointer(&storage[0]), start)
 a.off = start+size
 clear(unsafe.Slice((*byte)(p), size))
 return p
}
func (a *Tiny) Free(p unsafe.Pointer, size, align uintptr) {
 if uintptr(p)+size == uintptr(unsafe.Pointer(&storage[0]))+a.off {
  a.off = uintptr(p)-uintptr(unsafe.Pointer(&storage[0]))
 }
}
`

const generationPageProvider = `
var outer arena.Chunked
type PageArena struct { a *arena.Chunked }
func (p PageArena) AllocPages(n uintptr) unsafe.Pointer { return p.a.Alloc(n*4096, 4096) }
func (p PageArena) FreePages(b unsafe.Pointer, n uintptr) { p.a.Free(b, n*4096, 4096) }
`
