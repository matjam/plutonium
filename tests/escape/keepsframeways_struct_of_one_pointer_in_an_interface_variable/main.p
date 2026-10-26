package main

type Inner struct{ n int }
var keep *Inner
type Stringer interface{ String() string }
type D struct{ P *Inner }
func (d D) String() string { keep = d.P; return "d" }
func f() {
	var in Inner
	var s Stringer = D{&in}
	println(s.String())
}
func main() { f() }
