package main
type T struct{ a int }
func f(p *T) {}
func g() { task f(&T{}) }