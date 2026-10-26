package main

import "unsafe"

type T struct{ n int }

type (
	ptr      *T
	fn       func(int) int
	up       unsafe.Pointer
	wrap     struct{ p *T }
	wrapFn   struct{ f func() }
	wrapUP   struct{ p unsafe.Pointer }
	nested   struct{ w wrap }
	arr1     [1]*T
	arrWrap  [1]wrap
	own      owned[T]
	wrapOwn  owner struct{ o owned[T] }
	alias    = wrap
	named    wrap
	two      struct{ p, q *T }
	blank    struct{ p *T; _ int }
	scalar   struct{ n int }
	arr2     [2]*T
	arr0     [0]*T
	str      string
	slice    []*T
	iface    interface{ M() }
	wrapI    struct{ i interface{ M() } }
	integer  int
	empty    struct{}
	wrapSlc  struct{ s []byte }
)
