package main

type I interface{ M(p *int) }
type keeper struct{}
var saved *int
func (keeper) M(p *int) { saved = p }
type looker struct{}
func (looker) M(p *int) {}
func box[T any](x T) any { return x }
func call(i I) {
	x := 1
	i.M(&x)
}
func main() {
	_ = box(looker{})
	call(looker{})
	var k keeper
	_ = k
}
