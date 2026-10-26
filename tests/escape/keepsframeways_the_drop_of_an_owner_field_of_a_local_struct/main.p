package main

type O owner struct{ n int }
type W owner struct{ o O }
var keep *O
func (o *O) Drop() { keep = o }
func f() {
	var w W
	w.o.n = 1
}
func main() { f() }
