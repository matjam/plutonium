package main

type Inner struct{ n int }
var keep *Inner
func id(s struct{ p *Inner }) struct{ p *Inner } { return s }
func f() {
	var in Inner
	u := struct{ p *Inner }{&in}
	v := id(u)
	keep = v.p
}
func main() { f() }
