package main

import "pluto/arena"

func main() {
	a := arena.NewFixed(4096)
	pa := &a
	p := pa.New[int]()
	*p = 7
	a.Reset()
	println(*p)
}
