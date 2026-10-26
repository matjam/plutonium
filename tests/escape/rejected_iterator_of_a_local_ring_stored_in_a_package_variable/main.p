package main

import "pluto/iter"
var g iter.Seq2[int, *int]
func f() {
	var r ring[*int, 4]
	g = r.All()
}
func main() { f() }
