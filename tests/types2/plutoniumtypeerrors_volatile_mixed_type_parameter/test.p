package main
type D[T ~int | ~float64] struct{ v volatile[T] }