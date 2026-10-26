package main

type C struct{ n int }
func (c C) Get() int { return c.n }
var c0 = C{3}
var F = c0.Get
func main() { _ = F() }
