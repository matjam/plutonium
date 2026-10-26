package main

type T struct{}
func (T) M(a int = 1) {}
type I interface{ M(a int) }
func g() { var i I = T{}; i.M() }
