package main

import "m/sys"

func f() int32 {
	fd := sys.Open()
	o := sys.Other(fd) // owner to owner: moves
	_ = o
	g := sys.Open()
	return int32(g) // owner to plain value: reads
}
