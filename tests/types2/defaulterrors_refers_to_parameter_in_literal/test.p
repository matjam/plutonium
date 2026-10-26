package main

func f(a int, g func() int = func() int { return a }) {}
