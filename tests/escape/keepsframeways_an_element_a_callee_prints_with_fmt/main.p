package main

import "pluto/fmt"
type Inner struct{ n int }
var keep *Inner
func (i *Inner) String() string { keep = i; return "inner" }
func g(p *[2]Inner) { fmt.Println(&p[1]) }
func f() {
	var arr [2]Inner
	g(&arr)
}
func main() { f() }
