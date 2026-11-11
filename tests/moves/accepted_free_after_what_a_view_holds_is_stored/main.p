package main

import "pluto/vec"

// A call that stores only what was loaded through a view (the elements of
// a slice, copied) does not keep the view, so a later free of the owner is
// no error (language spec section 18.3).

type T struct{ n int }

var ga, gb T

type W owner struct{ out vec.Vec[int] }

func put(w *W, s []int) {
	for _, x := range s {
		w.out.Push(x)
	}
}

func main() {
	w := new(W)
	v := vec.New[int]()
	v.Push(1)
	put(&*w, v.Slice())
	v.Free()
	println(w.out.Slice()[0])

	ga.n, gb.n = 2, 3
	ts := vec.New[*T]()
	ts.Push(&ga)
	ts.Push(&gb)
	sum := 0
	fn := func(t *T) { sum += t.n }
	for _, t := range ts.Slice() {
		fn(t)
	}
	ts.Free()
	println(sum)
}
