package main

import "pluto/vec"

// An element of a map or container that is an array has no address of its
// own: indexing it (m[k][i], v[i][j]) reads a copy.
func main() {
	m := map[string][2]int{"a": {1, 2}}
	println(m["a"][1], m["b"][0])
	v := vec.New[[2]int]()
	v.Push([2]int{3, 4})
	println(v[0][0], v[0][1])
	v.Drop()
}
