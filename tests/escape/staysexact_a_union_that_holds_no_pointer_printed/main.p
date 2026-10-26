package main

import "pluto/fmt"
type Named struct{ name string }
var last *Named
func (n *Named) String() string { last = n; return "named" }
type U union {
	None
	N int
}
func f() {
	fmt.Println(U.N(1))
}
func main() { f() }
