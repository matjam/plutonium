package main

import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
type W struct {
	P   *Inner
	pad int
}
func (w W) String() string { keep = w.P; return "w" }
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
type Outer struct{ W W }
func f() {
	fmt.Println(Outer{W{nil, 0}})
}
func main() { f() }
