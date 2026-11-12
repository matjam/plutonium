package main

// The size of aligned is not a constant (E2109).

var n = 64

var buf aligned[[4]byte, n]

func main() {}
