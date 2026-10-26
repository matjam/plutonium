package main

type G struct{ p *[1]*int }
func (g *G) Index(i int) *int { return g.p[i] }
var keep *int
func f() {
	x := 42
	arr := [1]*int{&x}
	g := G{&arr}
	keep = g[0]
}
func main() { f() }
