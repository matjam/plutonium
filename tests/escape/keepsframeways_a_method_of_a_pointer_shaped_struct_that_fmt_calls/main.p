package main

import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
type Outer struct{ D D }
func f() {
	var in Inner
	fmt.Println(Outer{D{&in}})
}
func main() { f() }
