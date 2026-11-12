package main

import "pluto/vec"

// The range statement reads the next element through v.Slice() at each
// iteration, after v.Push may have freed the array (language spec section
// 18.3).

func main() {
	var v vec.Vec[int]
	v.Push(7)
	v.Push(8)
	for _, x := range v.Slice() {
		for j := 0; j < 100; j++ {
			v.Push(j)
		}
		println(x)
	}
}
