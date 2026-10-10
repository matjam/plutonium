package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
	"pluto/vec"
	"unsafe"
)

type big struct {
	a, b, c int64
	s       string
}

// counting wraps an allocator and counts live bytes.
type counting struct {
	inner alloc.Allocator
	live  int
	calls int
}

func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.live += int(size)
	c.calls++
	return c.inner.Alloc(size, align)
}

func (c *counting) Free(p unsafe.Pointer, size, align uintptr) {
	c.live -= int(size)
	c.inner.Free(p, size, align)
}

func main() {
	ca := &counting{inner: alloc.Default()}
	v := vec.New[int](ca)
	for i := 0; i < 1000; i++ {
		v.Push(i * i)
	}
	fmt.Printf("len=%d cap=%d at10=%d live=%d calls=%d\n", v.Len(), v.Cap(), v.At(10), ca.live, ca.calls)
	s := 0
	for _, x := range v.Slice() {
		s += x
	}
	fmt.Printf("sum=%d\n", s)
	x, ok := v.Pop()
	fmt.Printf("pop=%d %t len=%d\n", x, ok, v.Len())
	v.Truncate(5)
	v.Insert(0, -1)
	v.Insert(6, 99)
	v.Set(2, 42)
	*v.Ptr(3) += 1000
	fmt.Printf("%d %d %d %d %d %d %d\n", v.At(0), v.At(1), v.At(2), v.At(3), v.At(4), v.At(5), v.At(6))
	r := v.Remove(1)
	sr := v.SwapRemove(0)
	last, _ := v.Last()
	fmt.Printf("removed %d %d len=%d first=%d last=%d\n", r, sr, v.Len(), v.At(0), last)
	v.Append(7, 8, 9)
	fmt.Printf("len=%d last=%d\n", v.Len(), v.At(v.Len()-1))
	v.Clear()
	_, ok = v.Pop()
	fmt.Printf("cleared len=%d pop ok=%t cap=%d\n", v.Len(), ok, v.Cap())
	v.Free()
	fmt.Printf("freed live=%d len=%d cap=%d\n", ca.live, v.Len(), v.Cap())
	v.Push(5)
	fmt.Printf("reuse %d live=%d\n", v.At(0), ca.live)
	v.Free()

	// Vec of structs on an arena.
	// Backed by uint64s so the buffer is 8-aligned and padding is predictable.
	var mem [1 << 13]uint64
	ar := arena.New(unsafe.Slice((*byte)(unsafe.Pointer(&mem)), len(mem)*8))
	bv := vec.WithCapacity[big](2, &ar)
	for i := 0; i < 100; i++ {
		bv.Push(big{a: int64(i), b: int64(2 * i), c: int64(3 * i), s: "x"})
	}
	t := int64(0)
	for i := 0; i < bv.Len(); i++ {
		p := bv.Ptr(i)
		t += p.a + p.b + p.c + int64(len(p.s))
	}
	fmt.Printf("big len=%d total=%d used=%d\n", bv.Len(), t, ar.Used())

	// Arena basics.
	ar.Reset()
	p1 := ar.Alloc(3, 1)
	p2 := ar.Alloc(8, 8)
	fmt.Printf("align ok=%t gap=%d used=%d\n", uintptr(p2)%8 == 0, uintptr(p2)-uintptr(p1), ar.Used())
	ar.Free(p2, 8, 8)
	fmt.Printf("after free last used=%d\n", ar.Used())
	m := ar.Mark()
	ar.Alloc(100, 16)
	ar.Release(m)
	fmt.Printf("after release used=%d avail=%d cap=%d\n", ar.Used(), ar.Available(), ar.Cap())
	fmt.Printf("too big nil=%t\n", ar.Alloc(1<<20, 1) == nil)
	// Default allocator scoped to the arena: new and make use it.
	r2 := alloc.Use(&ar)
	q := new(big)
	buf := make([]byte, 10)
	r2.Restore()
	inArena := uintptr(unsafe.Pointer(q)) >= uintptr(unsafe.Pointer(&mem[0])) && uintptr(unsafe.Pointer(q)) < uintptr(unsafe.Pointer(&mem[0]))+uintptr(len(mem))
	fmt.Printf("new in arena=%t len=%d\n", inArena, len(buf))
	// Dirty memory is zeroed again after Reset.
	ar.Reset()
	z := (*[16]byte)(ar.Alloc(16, 1))
	z[3] = 7
	ar.Reset()
	z = (*[16]byte)(ar.Alloc(16, 1))
	fmt.Printf("zeroed=%d\n", z[3])

	// Builder on an arena.
	ar.Reset()
	var b strings.Builder
	b.Init(&ar)
	b.Grow(2)
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&b, "%d,", i)
	}
	fmt.Printf("%s len=%d used=%d\n", b.String()[:20], b.Len(), ar.Used())
}
