package main

type C struct{ n int }
func (c C) Get() int { return c.n }
func f(c C) func() int { return c.Get }
var saved func() int
func main() { saved = f(C{}) }
