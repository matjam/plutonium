package main

import "pluto/vec"

func sumv(v vec.Vec[int]) {
	t := 0
	for _, x := range v.Slice() {
		t += x
	}
	println(t)
}

var keep []int

func main() {
	v := vec.New[int]()
	v.Push(1)
	keep = v.Slice()
	task sumv(v)
	keep[0] = 9
}
