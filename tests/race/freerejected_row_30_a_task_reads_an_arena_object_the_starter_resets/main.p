package main

import (
	"pluto/alloc"
	"pluto/arena"
)

type P struct{ x int }

var (
	ar = arena.NewFixed(1024)
	p  *P
)

func reader(done chan int) {
	done <- p.x
}

func main() {
	p = alloc.New[P](&ar)
	p.x = 1
	done := make(chan int)
	task reader(done)
	ar.Reset() // frees the P that reader reads
	println(<-done)
}
