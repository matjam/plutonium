package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/vec"
	"unsafe"
)

// counting wraps an allocator and counts its allocations.
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

type pair struct {
	a, b int
	s    string
}

type big struct {
	a, b, c, d, e, f, g int
}

var (
	orderMem [4]string
	order    []string
)

func trace(s string, v int) int {
	order = order[:len(order)+1]
	order[len(order)-1] = s
	return v
}

func tracedAlloc(c *counting) *counting {
	order = order[:len(order)+1]
	order[len(order)-1] = "alloc"
	return c
}

func main() {
	c := &counting{inner: alloc.Default()}
	var a alloc.Allocator = c

	// new(T, a): a concrete allocator and an interface value.
	p := new(pair, c)
	q := new(pair, a)
	fmt.Printf("T: %d %d %q %d %q calls=%d live=%d\n", p.a, p.b, p.s, q.a, q.s, c.calls, c.live)

	// new(v, a): a composite literal, constants, a variable, a large value.
	r := new(pair{a: 3, b: 4, s: "y"}, c)
	n := new(42, a)
	s := new("str", c)
	v := pair{a: 7, s: "v"}
	w := new(v, c)
	v.a = 0
	g := new(big{a: 1, g: 9}, c)
	fmt.Printf("v: %d %d %q %d %q %d %q %d %d calls=%d live=%d\n",
		r.a, r.b, r.s, *n, *s, w.a, w.s, g.a, g.g, c.calls, c.live)

	// The value is evaluated before the allocator.
	order = orderMem[:0]
	t := new(trace("value", 5), tracedAlloc(c))
	fmt.Printf("order: %d %s %s %d\n", len(order), order[0], order[1], *t)

	// vec.Make and vec.WithCapacity with an allocator.
	b1 := vec.Make[int](3, c)
	b2 := vec.WithCapacity[byte](8, a)
	b2.Append(0, 0)
	b1[2] = 11
	fmt.Printf("make: %d %d %d %d %d calls=%d\n", b1.Len(), b1.Cap(), b1[2], b2.Len(), b2.Cap(), c.calls)

	// Without an allocator, new and vec.Make use the current default.
	d := &counting{inner: alloc.Default()}
	restore := alloc.Use(d)
	e := new(pair{s: "default"})
	m := vec.Make[int](2)
	restore.Restore()
	fmt.Printf("default: %q %d calls=%d other=%d\n", e.s, m.Len(), d.calls, c.calls)

	alloc.Free(p, a)
	alloc.Free(q, a)
	alloc.Free(r, a)
	alloc.Free(n, a)
	alloc.Free(s, a)
	alloc.Free(w, a)
	alloc.Free(g, a)
	alloc.Free(t, a)
	// Only the two slices are left.
	fmt.Printf("freed live=%d\n", c.live)
}
