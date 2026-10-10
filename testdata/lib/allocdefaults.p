package main

import (
	"pluto/alloc"
	"pluto/errors"
	"pluto/fmt"
	"pluto/hash"
	"pluto/os"
	"pluto/strings"
	"pluto/vec"
	"unsafe"
)

// The standard library's allocating functions take their allocator as a
// trailing parameter that defaults to alloc.Default() (language spec
// sections 5.1 and 6.1). A counting allocator shows which one each call
// used.

type counter struct {
	a      alloc.Allocator
	allocs int
	frees  int
}

func (c *counter) Alloc(size, align uintptr) unsafe.Pointer {
	c.allocs++
	return c.a.Alloc(size, align)
}

func (c *counter) Free(p unsafe.Pointer, size, align uintptr) {
	c.frees++
	c.a.Free(p, size, align)
}

type pair struct{ a, b int }

var defCounter, expCounter counter

func main() {
	sys := alloc.Default()
	// Package-level: the default allocator is a package variable, and errors
	// keep the allocator they came from, so neither may point into main's
	// frame.
	def := &defCounter // installed as the default
	exp := &expCounter // passed explicitly
	def.a, exp.a = sys, sys
	alloc.SetDefault(def)
	report := func(what string) {
		fmt.Printf("%-12s default %d/%d explicit %d/%d\n", what, def.allocs, def.frees, exp.allocs, exp.frees)
	}

	p := alloc.New[pair]()
	q := alloc.New[pair](exp)
	p.a, q.b = 1, 2
	alloc.Free(p)
	alloc.Free(q, exp)
	report("New/Free")

	r := alloc.Raw(24, 8)
	s := alloc.Raw(24, 8, exp)
	def.Free(r, 24, 8)
	exp.Free(s, 24, 8)
	report("Raw")

	v := vec.New[int]()
	w := vec.WithCapacity[int](4, exp)
	for i := 0; i < 3; i++ {
		v.Push(i)
		w.Push(10 * i)
	}
	sq := w.Map(func(x int) int { return x * x })
	fmt.Println(v.Len(), w.Len(), sq.At(2))
	v.Free()
	w.Free()
	sq.Free()
	report("vec")

	m := hash.NewMap[int, string](4)
	m.Set(1, "one")
	set := hash.NewSet[int](4, exp)
	set.Add(7)
	fmt.Println(m.Len(), set.Len(), set.Has(7))
	m.Free()
	set.Free()
	report("hash")

	var b strings.Builder
	b.Init(exp)
	b.Grow(8)
	b.WriteString("built on explicit")
	var b2 strings.Builder
	b2.Grow(8)
	b2.WriteString("built on default")
	fmt.Println(b.String(), "/", b2.String())
	b.Free()
	b2.Free()
	report("Builder")

	_, err := os.Open("/nonexistent/allocdefaults")
	_, err2 := os.Open("/nonexistent/allocdefaults", exp)
	var pe *os.PathError
	fmt.Println(err, errors.As(err2, &pe), pe.Op, errors.Is(err2, os.ErrNotExist))
	errors.Free(err)
	errors.Free(err2)
	report("os.Open")

	pe = os.NewPathError("stat", "x", os.ErrInvalid, exp)
	fmt.Println(pe)
	errors.Free(pe)
	report("PathError")

	// Errorf and Join are variadic, so they cannot have a defaulted
	// allocator: they use the default one, which alloc.Use replaces.
	e1 := fmt.Errorf("plain %d", 1)
	restore := alloc.Use(exp)
	e2 := fmt.Errorf("wrapped: %w", e1)
	e3 := errors.Join(e2, errors.New("other"))
	restore.Restore()
	fmt.Println(e3)
	errors.Free(e3)
	report("Errorf/Join")
}
