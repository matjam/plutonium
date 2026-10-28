package main

import (
	"pluto/hash"
	"pluto/vec"
)

type Conn owner struct{ id int }

func main() {
	s := hash.NewSet[Conn](0)
	_ = s
	var buf [4]Conn
	f := vec.NewFixed(buf[:])
	_ = f
}
