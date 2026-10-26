package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"pluto/strings"
	"unsafe"
)
type counting struct{ next alloc.Allocator }
func (c *counting) Alloc(n, a uintptr) unsafe.Pointer       { return c.next.Alloc(n, a) }
func (c *counting) Free(p unsafe.Pointer, n, a uintptr)     { c.next.Free(p, n, a) }
func f(buf []byte) {
	ar := arena.NewUnsafe(buf) // manual: the program owns its lifetime
	var b strings.Builder
	b.Init(&ar)
	b.WriteString("x")
	fmt.Println(b.String())
	c := counting{next: alloc.Default()}
	r := alloc.Use(&c)
	s := "a" + b.String()
	fmt.Println(s)
	r.Restore()
}
var mem [4096]byte
func main() { f(mem[:]) }
