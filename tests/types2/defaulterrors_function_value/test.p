package main

func f(a int, b int = 1) {}
func g() { h := f; h(1) }
