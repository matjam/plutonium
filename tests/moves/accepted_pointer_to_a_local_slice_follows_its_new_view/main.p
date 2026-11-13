package main

import "pluto/vec"

// p points to the local s. After s = w.Slice(), *p is a view of w, not of
// v: v.Push(2) frees nothing *p looks at, so reading *p after it is valid
// (language spec section 18.3).

func main() {
	v := vec.New[int]()
	v.Push(1)
	w := vec.New[int]()
	w.Push(5)
	s := v.Slice()
	p := &s
	s = w.Slice()
	v.Push(2)
	println(len(*p), (*p)[0])
}
