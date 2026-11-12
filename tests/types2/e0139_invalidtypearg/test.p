package main

type T[P ~int] struct{}

var _ T[float64]

func main() {}
