package main

import "pluto/alloc"
import "pluto/vec"

func sumv(v vec.Vec[int]) {
	t := 0
	for _, x := range v.Slice() {
		t += x
	}
	println(t)
}

type ref struct{ s []int }

func main() {
	v := vec.New[int]()
	v.Push(1)
	r := alloc.New[ref]()
	r.s = v.Slice()
	task sumv(v)
	r.s[0] = 9
}
