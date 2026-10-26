package main

var heapArr [1]*int
type B struct{ p *[1]*int }
func (b *B) init() { b.p = &heapArr }
func f() {
	var b B
	b.init()
	x := 42
	b.p[0] = &x
}
func g() int {
	var arr [1]*int
	b := B{&arr}
	return len(b.p)
}
func main() { f(); println(g()) }
