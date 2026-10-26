package main

type T struct{ n int }
func (t T) M(b int = t.n) {}
