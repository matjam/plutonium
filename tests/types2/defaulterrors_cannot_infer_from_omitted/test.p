package main

func f[T any](a T = *new(T)) {}
func g() { f() }
