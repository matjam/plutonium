package main

import (
	"pluto/alloc"
	"pluto/arena"
)
func install() alloc.Restore {
	a := arena.NewFixed(64)
	return alloc.Use(&a)
}
func main() { install().Restore() }
