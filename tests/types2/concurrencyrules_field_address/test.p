package main
type T struct{ a [2]int }
func f(p *int) {}
func g() { var t T; task f(&t.a[1]) }