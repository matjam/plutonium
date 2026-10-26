package main

type T struct{ a int }
type S struct{ s string; n int }
var P = &T{1}
var L = []int{1, 2, 3}
var A any = S{"x", 2}
func main() { _, _, _ = P, L, A }
