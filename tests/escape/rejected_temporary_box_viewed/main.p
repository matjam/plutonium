package main

type N struct{ v int }
func f() *N { return (*N)(new(N)) }
func main() { _ = f() }
