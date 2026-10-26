package main

import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
func (i *Inner) String() string { keep = i; return "inner" }
func f() {
	var in Inner
	fmt.Println([]*Inner{&in})
}
func main() { f() }
