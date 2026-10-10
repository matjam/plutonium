package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"unsafe"
)

// counting wraps an allocator and counts its allocations.
type counting struct {
	inner alloc.Allocator
	live  int
	calls int
}

var defaultCounter counting

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

var order []string

func trace(s string, v int) int {
	order = append(order, s)
	return v
}

func tracedAlloc(c *counting) *counting {
	order = append(order, "alloc")
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
	order = make([]string, 0, 4)
	t := new(trace("value", 5), tracedAlloc(c))
	fmt.Printf("order: %d %s %s %d\n", len(order), order[0], order[1], *t)

	// make with an allocator, with and without a capacity.
	b1 := make([]int, 3, c)
	b2 := make([]byte, 2, 8, a)
	b1[2] = 11
	fmt.Printf("make: %d %d %d %d %d calls=%d\n", len(b1), cap(b1), b1[2], len(b2), cap(b2), c.calls)

	// Without an allocator, new and make use the current default.
	// The default allocator is a package variable, so the allocator put
	// there must not live in main's frame.
	d := &defaultCounter
	d.inner = alloc.Default()
	restore := alloc.Use(d)
	e := new(pair{s: "default"})
	m := make([]int, 2)
	restore.Restore()
	fmt.Printf("default: %q %d calls=%d other=%d\n", e.s, len(m), d.calls, c.calls)

	alloc.FreeIn(a, p)
	alloc.FreeIn(a, q)
	alloc.FreeIn(a, r)
	alloc.FreeIn(a, n)
	alloc.FreeIn(a, s)
	alloc.FreeIn(a, w)
	alloc.FreeIn(a, g)
	alloc.FreeIn(a, t)
	// Only the two slices are left.
	fmt.Printf("freed live=%d\n", c.live)
}
