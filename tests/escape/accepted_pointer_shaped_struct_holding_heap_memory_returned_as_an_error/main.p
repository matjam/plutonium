package main

import "pluto/alloc"
type W struct{ p *int }
func (W) Error() string { return "w" }
func f() error { return W{alloc.New[int]()} }
func main() { _ = f() }
