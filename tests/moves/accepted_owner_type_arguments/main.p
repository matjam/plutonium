package main

import (
	"pluto/hash"
	"pluto/vec"
)

// Owner types as explicit type arguments of generic calls and types. A type
// argument is a type, not a read of a container's element.

type Box[T owner any] owner struct{ v T }

func keep[T owner any](x T) T { return x }

func main() {
	m := hash.NewMap[string, vec.Vec[int]](0)
	defer m.Free()
	m.Set("a", vec.New[int]())

	vvv := vec.New[vec.Vec[vec.Vec[int]]]()
	defer vvv.Free()

	w := keep[vec.Vec[int]](vec.New[int]())
	b := Box[vec.Vec[int]]{v: w}
	var c Box[vec.Vec[int]] = b
	println(m.Len(), vvv.Len(), c.v.Len())
}
