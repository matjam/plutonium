package main
import (
	"pluto/alloc"
	"unsafe"
)

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type View owner struct {
	c *Counter
}

func newView(c *Counter) View { return View{c: c} }

func read(v View) { println(v.c.n, unsafe.Sizeof(v)) }

func main() {
	c := alloc.New[Counter]()
	task read(newView(c))
	bump(c)
}