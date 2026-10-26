package main

func f() any { return make(chan int) }
func main() { _ = f() }
