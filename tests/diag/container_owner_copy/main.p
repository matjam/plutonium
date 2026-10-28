package main

import (
	"pluto/hash"
	"pluto/vec"
)

type Conn owner struct{ id int }

func first(v *vec.Vec[Conn]) int {
	c := v.At(0)
	return c.id
}

func main() {
	v := vec.New[Conn]()
	v.Push(Conn{1})
	println(first(&v))
	m := hash.NewMap[string, Conn](0)
	m.Set("a", Conn{2})
	c, _ := m.Get("a")
	println(c.id)
}
