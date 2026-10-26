package main
import "pluto/alloc"

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

func read(v View) { println(v.c.n) }

func main() {
	c := alloc.New[Counter]()
	task read(View{c: c})
	bump(c)
}