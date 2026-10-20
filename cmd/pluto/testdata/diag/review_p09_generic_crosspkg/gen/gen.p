package gen

import "unsafe"

var Kept unsafe.Pointer

type Keeper interface{ Keep() }

type g[T any] struct{ v T }

func (x *g[T]) Keep() { Kept = unsafe.Pointer(x) }

// H is the only place g is instantiated: in this package's type declaration.
type H struct {
	g[int]
	Pad int
}
