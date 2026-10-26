package main

type G struct{ n int }
var keep *int
func (g *G) SetIndex(i int, p *int) { keep = p }
func (g *G) Index(i int) *int       { return nil }
var gg G
func f() {
	x := 42
	gg[0] = &x
}
func main() { f() }
