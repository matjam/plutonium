package main
import "pluto/alloc"

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

var keep []*Counter

func (v *View) Init() {
	c := alloc.New[Counter]()
	keepArr[0] = c; keep = keepArr[:]
	v.c = c
}

func read(v View) { println(v.c.n) }

func main() {
	var v View
	v.Init()
	bump(keep[0])
	task read(v)
}