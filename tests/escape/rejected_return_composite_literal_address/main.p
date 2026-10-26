package main

type T struct{ a int }
func f() *T { return &T{1} }
func main() { _ = f() }
