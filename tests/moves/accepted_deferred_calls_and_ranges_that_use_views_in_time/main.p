package main

import "pluto/vec"

// A deferred call frees where the function returns, after the body: the
// body may use the views until then. A call deferred later runs earlier,
// and an argument read at the defer statement is a copy (language spec
// sections 12.19 and 18.3). A range whose body frees nothing, or leaves
// the loop right after the free, reads no freed memory.

func show(p *int) { println(*p) }

func main() {
	var v vec.Vec[int]
	v.Push(7)
	p := v.Ptr(0)
	defer v.Free()
	defer show(p)
	defer println(*p)
	defer func() { println(*p) }()
	println(*p)

	var w vec.Vec[int]
	w.Push(1)
	q := w.Ptr(0)
	defer func() { w.Free() }()
	println(*q)

	var u vec.Vec[int]
	u.Push(1)
	u.Push(2)
	for _, x := range u.Slice() {
		println(x)
	}
	for _, x := range u.Slice() {
		if x == 2 {
			u.Push(3)
			break
		}
	}
	u.Free()
}
