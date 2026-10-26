package main
//pu:percpu
var ticks uint64

type C struct{ n uint64 }

func (c C) save() { ticks = c.n }

func main() {
	c := C{n: ticks}
	c.save()
}