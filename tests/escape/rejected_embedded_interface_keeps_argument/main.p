package main

type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
type wrap struct{ Keeper }
func main() {
	w := wrap{k{}}
	x := 1
	w.Keep(&x)
}
