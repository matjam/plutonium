package main

type T struct{}
func (T) M(a int = 1) {}
func g() { T.M(T{}) }
