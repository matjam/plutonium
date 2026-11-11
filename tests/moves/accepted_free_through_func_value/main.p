package main

import "pluto/vec"

// A func value whose candidates are known and free nothing leaves the
// views alone, and an argument whose type leads to no memory that may be
// freed is not a free (language spec section 18.3).

func run(f func(*vec.Vec[int]) int, v *vec.Vec[int]) int { return f(v) }

func size(v *vec.Vec[int]) int { return v.Len() }

var hook func(*int)

func main() {
	v := vec.New[int]()
	v.Push(0)
	s := v.Slice()
	println(run(size, &v))
	println(s[0])

	n := 1
	w := vec.New[int]()
	w.Push(0)
	t := w.Slice()
	if hook != nil {
		hook(&n)
	}
	println(t[0], n)
}
