package types2_test

import "testing"

const allocatorDecls = `package p

import "unsafe"

type Arena struct{ off uintptr }

func (a *Arena) Alloc(size, align uintptr) unsafe.Pointer { return nil }
func (a *Arena) Free(p unsafe.Pointer, size, align uintptr) {}

type Allocator interface {
	Alloc(size, align uintptr) unsafe.Pointer
	Free(p unsafe.Pointer, size, align uintptr)
}

type T struct{ x int }
`

// TestNewMakeAllocator checks the allocator argument of new and make
// (language spec section 4.2): it is told from a capacity by its type.
func TestNewMakeAllocator(t *testing.T) {
	mustCheck(t, allocatorDecls+`
func f[E any, N ~int](n N, a Allocator) {
	var ar Arena
	_ = new(T)
	_ = new(T, &ar)
	_ = new(T, a)
	_ = new(T{x: 1}, a)
	_ = new(T, nil)
	var s []int = make([]int, 1)
	s = make([]int, 1, 2)
	s = make([]int, 1, &ar)
	s = make([]int, 1, 2, a)
	s = make([]int, n, n, a)
	_ = s
	_ = make([]E, n, a)
}
`)
}

func TestNewMakeAllocatorErrors(t *testing.T) {
	const notAllocator = "is not an allocator: the final argument of"
	cases := []struct{ name, body, want string }{
		{"new with a non-allocator", "var _ = new(T, \"x\")", notAllocator + " new must have the methods"},
		{"new with an incomplete allocator", "type half struct{}\nfunc (half) Alloc(size, align uintptr) unsafe.Pointer { return nil }\nvar _ = new(T, half{})",
			notAllocator + " new"},
		{"new with too many arguments", "var a Allocator\nvar _ = new(T, a, a)", "too many arguments for new(T, a, a)"},
		{"make with a non-allocator", "var _ = make([]int, 1, \"x\")", notAllocator + " make"},
		{"make without a length", "var a Allocator\nvar _ = make([]int, a)", "expects 2 or 3 arguments and an optional allocator; found 2"},
		{"make with too many sizes", "var _ = make([]int, 1, 2, 3)", "expects 2 or 3 arguments and an optional allocator; found 4"},
		{"make with an allocator in the middle", "var a Allocator\nvar _ = make([]int, 1, a, a)", "index a (variable of interface type Allocator) must be integer"},
		{"make with a negative capacity", "var a Allocator\nvar _ = make([]int, 1, -2, a)", "must not be negative"},
		{"make with swapped sizes", "var a Allocator\nvar _ = make([]int, 3, 2, a)", "length and capacity swapped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { mustFail(t, allocatorDecls+c.body+"\n", c.want) })
	}
}
