package main

import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
func (i *Inner) String() string { keep = i; return "inner" }
type U union {
	None
	P *Inner
}
func f() {
	var in Inner
	fmt.Println(U.P(&in))
}
func main() { f() }
