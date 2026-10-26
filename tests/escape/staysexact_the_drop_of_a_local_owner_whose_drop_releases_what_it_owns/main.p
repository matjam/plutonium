package main

type O owner struct{ p owned[int] }
func (o *O) Drop() { o.p = nil }
func f() int {
	var o O
	o.p = new(int)
	return *o.p
}
func main() { println(f()) }
