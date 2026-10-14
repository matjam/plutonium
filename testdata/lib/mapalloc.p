// Where a map's memory comes from (language spec section 7.10): make with
// a size hint, an allocator, or both (an argument is a size if it is an
// integer, an allocator otherwise); literals and make without an allocator
// use the current one, which alloc.Use changes; the box and the table are
// returned to the allocator they came from when the last copy of the map
// goes, whatever the current allocator is by then.

package main

import (
	"pluto/alloc"
	"pluto/arena"
	"pluto/fmt"
	"unsafe"
)

type counting struct {
	inner        alloc.Allocator
	live, allocs int
}

func (c *counting) Alloc(size, align uintptr) unsafe.Pointer {
	c.live += int(size)
	c.allocs++
	return c.inner.Alloc(size, align)
}

func (c *counting) Free(p unsafe.Pointer, size, align uintptr) {
	c.live -= int(size)
	c.inner.Free(p, size, align)
}

var ca = &counting{inner: alloc.Default()}

func fill(m map[int]string, n int) {
	for i := 0; i < n; i++ {
		m[i] = fmt.Sprint("v", i)
	}
}

// fromCurrent makes a map from the current allocator.
func fromCurrent() map[int]string {
	return map[int]string{1: "one"}
}

func scoped() map[int]string {
	defer alloc.Use(ca).Restore()
	return fromCurrent()
}

func main() {
	// make(map[K]V, n, a): a size hint and an allocator.
	m := make(map[int]string, 100, ca)
	fill(m, 100)
	fmt.Println(len(m), m[42], ca.allocs > 0, ca.live > 0)
	m2 := m // a second reference to the same table
	m = nil
	fmt.Println(len(m2), ca.live > 0)
	m2 = nil
	fmt.Println("after the last reference:", ca.live)

	// make(map[K]V, a): an allocator and no hint; the table grows from it.
	before := ca.allocs
	g := make(map[string]int, ca)
	for i := 0; i < 1000; i++ {
		g[fmt.Sprint(i)] = i
	}
	fmt.Println(len(g), g["999"], ca.allocs-before > 2)
	clear(g)
	fmt.Println(len(g), ca.live > 0)
	g = nil
	fmt.Println("cleared and dropped:", ca.live)

	// A literal and make without an allocator use the current one.
	s := scoped()
	fmt.Println(s[1], ca.live > 0)
	s[2] = "two" // grows from the map's own allocator, not the current one
	s = nil
	fmt.Println("scoped:", ca.live)

	// An arena: freeing into it does nothing, and it is reset at once.
	ar := arena.NewFixed(64 << 10)
	am := make(map[int]int, 0, &ar)
	for i := 0; i < 500; i++ {
		am[i] = i * i
	}
	fmt.Println(len(am), am[499], ar.Used() > 0)
	am = nil
	ar.Reset()
	fmt.Println(ar.Used())

	// A zero hint, and a hint of a large constant.
	z := make(map[int]bool, 0)
	z[1] = true
	big := make(map[int]bool, 1<<12)
	big[7] = true
	fmt.Println(len(z), len(big))
}
