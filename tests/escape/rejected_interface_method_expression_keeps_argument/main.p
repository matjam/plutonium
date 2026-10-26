package main

type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	f := Keeper.Keep
	x := 1
	f(k{}, &x)
}
