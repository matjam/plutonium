package main

type C struct{ n int }
func (c C) Get() int { return c.n }
var F func() int
func main() {
	c := C{1}
	F = c.Get
}
