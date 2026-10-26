package main

type O owner struct{ n int }
var keep *O
func (o *O) Drop() { keep = o }
func f() {
	var o O
	o.n = 1
}
func main() { f() }
