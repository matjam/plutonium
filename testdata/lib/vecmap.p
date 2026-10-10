package main

import (
	"pluto/alloc"
	"pluto/fmt"
	"pluto/vec"
	"unsafe"
)

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

type point struct{ x, y int32 }

// lengths maps a Vec inside generic code, with the result type taken from
// the function's own type parameter.
func lengths[T any, N ~int | ~int64](v *vec.Vec[T], f func(T) N, a alloc.Allocator) vec.Vec[N] {
	return v.Map[N](f, a)
}

func main() {
	src := &counting{inner: alloc.Default()}
	dst := &counting{inner: alloc.Default()}
	names := vec.New[string](src)
	names.Append("alpha", "be", "gamma", "")

	// U inferred from the function argument.
	ls := names.Map(func(s string) int { return len(s) }, dst)
	fmt.Printf("lens len=%d cap=%d %d %d %d %d\n", ls.Len(), ls.Cap(), ls.At(0), ls.At(1), ls.At(2), ls.At(3))
	fmt.Printf("src live=%d calls=%d dst live=%d calls=%d\n", src.live, src.calls, dst.live, dst.calls)

	// U given explicitly; a struct element type.
	ps := ls.Map[point](func(n int) point { return point{int32(n), int32(n * n)} }, dst)
	p := ps.At(2)
	fmt.Printf("points len=%d at2=%d,%d dst live=%d\n", ps.Len(), p.x, p.y, dst.live)

	// Inside a generic function.
	ws := lengths(&names, func(s string) int64 { return int64(len(s)) * 100 }, dst)
	fmt.Printf("generic %d %d same alloc=%t\n", ws.At(0), ws.At(2), ws.Allocator() == alloc.Allocator(dst))

	// An empty Vec maps to an empty Vec without allocating.
	var empty vec.Vec[bool]
	es := empty.Map(func(b bool) string { return "x" }, dst)
	fmt.Printf("empty len=%d cap=%d dst calls=%d\n", es.Len(), es.Cap(), dst.calls)

	// The result grows from the allocator it was given.
	es.Push("grown")
	fmt.Printf("grown %s dst calls=%d\n", es.At(0), dst.calls)

	ls.Free()
	ps.Free()
	ws.Free()
	es.Free()
	names.Free()
	fmt.Printf("freed src=%d dst=%d\n", src.live, dst.live)
}
