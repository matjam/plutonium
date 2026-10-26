package main
func f(p *int) {}
func g() { x := 1; task f(&x) }