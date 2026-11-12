package main

type T1 chan int
type T2 chan int

var x T1
// Invalid assignment because both types are named
var _ T2 = x

func main() {}
