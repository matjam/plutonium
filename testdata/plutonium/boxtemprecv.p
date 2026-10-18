package main

// A pointer-receiver call on a field reached through a box (language spec
// sections 11.14 and 7.10). The box is a pointer, so the field belongs to
// the shared object whatever expression produced the box: a call result, a
// map element, a slice element, a field of another box. Each call must
// change the object, never a copy of it. A map element that is a struct
// value is not addressable, and the checker rejects those calls
// (cmd/pluto/testdata/diag/boxtemprecv.p).

import (
	"pluto/alloc"
	"pluto/vec"
)

type Box owner struct{ v vec.Vec[int] }

type Outer owner struct{ in synced[Box] }

type Counter struct{ n int }

func (c *Counter) Inc() { c.n++ }

type Holder owner struct{ c Counter }

var g synced[Box]
var o synced[Outer]
var h synced[Holder]

func get() synced[Box]       { return g }
func getO() synced[Outer]    { return o }
func getH() synced[Holder]   { return h }
func pick(i int) synced[Box] { return g }

func main() {
	g = alloc.Synced[Box]()
	o = alloc.Synced[Outer]()
	h = alloc.Synced[Holder]()
	o.in = g
	g.v.Push(1)
	println("bound", g.v.Len())

	get().v.Push(2)
	println("call result", g.v.Len())

	m := map[string]synced[Box]{"a": g}
	m["a"].v.Push(3)
	println("map element", g.v.Len())

	getO().in.v.Push(4)
	println("field of call result", g.v.Len())

	s := []synced[Box]{g}
	s[0].v.Push(5)
	println("slice element", g.v.Len())

	pick(1).v.Push(6)
	println("call with argument", g.v.Len())

	getH().c.Inc()
	getH().c.Inc()
	println("struct field", h.c.n)

	mh := map[string]synced[Holder]{"a": h}
	mh["a"].c.Inc()
	println("map struct field", h.c.n)
}
