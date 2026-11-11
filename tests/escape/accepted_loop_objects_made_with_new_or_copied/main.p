package main

import "pluto/vec"

// The fixes E3006 names for objects made in a loop: allocate each with
// new and keep its owner, keep the values in an owner, or use the view
// within the iteration (language spec sections 11.3 and 18.4).

type T struct{ n int }

func (t T) Get() int { return t.n }

func pack(xs ...int) []int { return xs }

func main() {
	var bs [3]owned[T]
	for i := 0; i < 3; i++ {
		bs[i] = new(T{n: i})
	}
	println(bs[2].n)

	v := vec.New[int]()
	for i := 0; i < 3; i++ {
		s := []int{i, i}
		v.Push(s[0])
		println(pack(i, i)[1])
	}
	println(v.Len())

	last := 0
	for i := 0; i < 3; i++ {
		t := T{n: i}
		f := t.Get
		last = f()
	}
	println(last)
}
