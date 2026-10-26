package main
import "pluto/alloc"

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

func (v *View) Init() {
	v.c = alloc.New[Counter]()
	set(&v.c)
}

var last *Counter

func set(p **Counter) { last = *p }

func read(v View) { println(v.c.n) }

func main() {
	var v View
	v.Init()
	bump(last)
	task read(v)
}