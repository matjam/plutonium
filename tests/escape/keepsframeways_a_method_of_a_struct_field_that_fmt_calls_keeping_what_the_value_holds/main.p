package main

import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
type W struct {
	P   *Inner
	pad int
}
func (w W) String() string { keep = w.P; return "w" }
type Outer struct{ W W }
func f() {
	var in Inner
	fmt.Println(Outer{W{&in, 1}})
}
func main() { f() }
