package main

type E1 struct { i int }
type E2 struct { i int }
type T struct { E1; E2 }

var x T
var _ = x.i

func main() {}
