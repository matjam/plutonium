package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/vec"
	"unsafe"
)

// new and make with an optional allocator (language spec section 4.2).

// A counting allocator: any type with Alloc and Free can be passed to new
// and make, not only an alloc.Allocator value.
type counter struct {
	a      alloc.Allocator
	allocs int
	bytes  uintptr
}

func (c *counter) Alloc(size, align uintptr) unsafe.Pointer {
	c.allocs++
	c.bytes += size
	return c.a.Alloc(size, align)
}

func (c *counter) Free(p unsafe.Pointer, size, align uintptr) { c.a.Free(p, size, align) }

// An interface of its own with the same methods also works.
type Pool interface {
	Alloc(size, align uintptr) unsafe.Pointer
	Free(p unsafe.Pointer, size, align uintptr)
}

type Node struct {
	key  int
	next *Node
}

func sum(xs []int) int {
	t := 0
	for _, x := range xs {
		t += x
	}
	return t
}

func main() {
	c := &counter{a: alloc.Default()}

	// new(T) from the default allocator and from an explicit one.
	n := new(Node)
	m := new(Node, c)
	fmt.Println(n.key, n.next == nil, m.key, m.next == nil, c.allocs, c.bytes)

	// new(expr) holds expr's value.
	p := new(41 + 1)
	q := new(Node{key: 7}, c)
	fmt.Println(*p, q.key, c.allocs, c.bytes)

	// vec.Make and vec.WithCapacity, Go's make([]T, n) and make([]T, n, c),
	// with and without an allocator.
	a := vec.Make[int](3)
	b := vec.WithCapacity[int](10)
	b.Append(0, 0)
	d := vec.Make[int](4, c)
	e := vec.WithCapacity[int](6, c)
	e.Push(0)
	fmt.Println(a.Len(), a.Cap(), b.Len(), b.Cap(), d.Len(), d.Cap(), e.Len(), e.Cap())
	fmt.Println(sum(a.Slice()), sum(b.Slice()), sum(d.Slice()), sum(e.Slice()), c.allocs, c.bytes)

	// Memory is zeroed, from an arena too.
	var mem [256]byte
	for i := range mem {
		mem[i] = 0xAA
	}
	ar := arena.NewUnsafe(mem[:])
	z := vec.Make[int64](4, &ar)
	w := new([3]uint8, &ar)
	fmt.Println(z[0], z[3], w[0], w[2], ar.Used())

	// An alloc.Allocator value, a Pool value and a variable capacity.
	var aa alloc.Allocator = c
	var pool Pool = c
	k := 5
	f := vec.WithCapacity[byte](k, aa)
	f.Append(0, 0)
	g := new(int32, pool)
	fmt.Println(f.Len(), f.Cap(), *g, c.allocs)

	// In a generic function the allocator and sizes keep their meaning.
	h := makeIn[uint16](3, c)
	fmt.Println(h.Len(), h.Cap(), c.allocs, c.bytes)
}

func makeIn[T any](n int, a alloc.Allocator) vec.Vec[T] {
	v := vec.WithCapacity[T](2*n, a)
	var zero T
	for range n {
		v.Push(zero)
	}
	return v
}
