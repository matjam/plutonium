package main

type Keeper interface{ Keep(p *int) }
type k struct{}
var saved *int
func (k) Keep(p *int) { saved = p }
func main() {
	var kp Keeper = k{}
	x := 1
	kp.Keep(&x)
}
