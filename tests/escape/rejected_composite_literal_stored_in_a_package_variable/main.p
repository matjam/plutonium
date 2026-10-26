package main

type T struct{ n int }
var G *T
func main() {
	G = &T{1}
}
