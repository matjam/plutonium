package main
func f(x shared[int]) {}
func g(x shared[int]) { task f(x) }