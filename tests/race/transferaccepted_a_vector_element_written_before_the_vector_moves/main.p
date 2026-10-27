package main

import "pluto/vec"

func sumv(v vec.Vec[int]) {
	t := 0
	for _, x := range v.Slice() {
		t += x
	}
	println(t)
}

func main() {
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	*p = 9
	task sumv(v)
}
