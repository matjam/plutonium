package main

func f[T any]() { f[*T]() }

func main() {}
