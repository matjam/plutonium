package main

import "pluto/arena"

func main() {
	a := arena.NewFixed(4096)
	p := a.New[int]()
	*p = 7
	defer func() { println(*p) }()
	defer a.Reset()
}
