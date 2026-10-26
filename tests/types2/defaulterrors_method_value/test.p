package main

type T struct{}
func (T) M(a int = 1) {}
func g() { m := T{}.M; m() }
