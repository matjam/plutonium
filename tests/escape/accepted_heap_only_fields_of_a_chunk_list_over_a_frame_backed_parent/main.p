package main

import (
	"pluto/alloc"
	"unsafe"
)
type Allocator interface{ Alloc(n uintptr) unsafe.Pointer }
type chunk struct{ prev *chunk }
type Chunked struct {
	parent      Allocator
	cur, spare  *chunk
}
func (a *Chunked) recycle(c *chunk) { c.prev = a.spare; a.spare = c }
func (a *Chunked) grow() {
	c := a.spare
	if c != nil {
		a.spare = c.prev
	} else {
		c = (*chunk)(a.parent.Alloc(64))
	}
	c.prev = a.cur
	a.cur = c
}
func (a *Chunked) release() {
	c := a.cur
	a.cur = c.prev
	a.recycle(c)
}
func (a *Chunked) Alloc(n uintptr) unsafe.Pointer {
	a.grow()
	return unsafe.Pointer(a.cur)
}
type heap struct{}
func (heap) Alloc(n uintptr) unsafe.Pointer { return unsafe.Pointer(alloc.New[[64]byte]()) }
type counting struct {
	under Allocator
	n     int
}
func (c *counting) Alloc(n uintptr) unsafe.Pointer {
	c.n++
	return c.under.Alloc(n)
}
func main() {
	parent := counting{under: heap{}}
	a := Chunked{parent: &parent}
	_ = a.Alloc(8)
	var inner Chunked
	inner.parent = &a
	_ = inner.Alloc(8)
	a.release()
}
