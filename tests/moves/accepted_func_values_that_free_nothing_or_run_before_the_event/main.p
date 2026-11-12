package main

import (
	"pluto/arena"
	"pluto/vec"
)

// Calls of literals and method values that free nothing leave the views
// alone; a literal called before its owner is moved, or one that only
// gives the owner a new value, is valid (language spec sections 18.3 and
// 18.5).

type L owner struct{ x int }

func eat(l L) {}

func apply(f func()) { f() }

func id(f func()) func() { return f }

type S struct{ f func() }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	show := func() { println(*p, v.Len()) }
	show()
	func() { println(v.Len()) }()
	apply(func() { println(v.Len()) })
	n := v.Len
	println(n(), *p)
	get := func() *int { return v.Ptr(0) }
	q := get()
	println(*q)
	v.Free()

	a := arena.NewFixed(4096)
	r := a.New[int]()
	*r = 3
	use := func() { println(*r) }
	use()
	a.Reset()

	w := L{x: 1}
	read := func() int { return w.x }
	set := func() { w = L{x: 2} }
	println(read())
	eat(w)
	set()

	u := L{x: 4}
	s := S{f: func() { println(u.x) }}
	defer s.f()
	defer id(func() { println(u.x) })()
	eat(u)
	u = L{x: 5}
}
