package main

import "pluto/fmt"
type Named struct{ name string }
var last *Named
func (n *Named) String() string { last = n; return "named" }
type Pair struct {
	a int
	s string
}
func f() {
	var p Pair
	fmt.Println(1, "x", 2.5, p, []int{1, 2}, [2]string{"a", "b"})
}
func main() { f() }
