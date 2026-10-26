package main
import (
	"pluto/alloc"
	"unsafe"
)

type Counter struct{ n int }

func bump(c *Counter) { c.n++ }

type Pool owner struct {
	items []Counter
	a     alloc.Allocator
}

func grow(a alloc.Allocator, n int) []Counter {
	var c Counter
	p := a.Alloc(uintptr(n)*unsafe.Sizeof(c), unsafe.Alignof(c))
	return unsafe.Slice((*Counter)(p), n)
}

func (p *Pool) Init(n int) {
	p.a = alloc.Default()
	items := grow(p.a, n)
	p.items = items[:n]
	for i := range p.items {
		bump(&p.items[i])
	}
}

func (p *Pool) Drop() {
	p.items = nil
}

func use(p Pool) { println(len(p.items)) }

func main() {
	var p Pool
	p.Init(4)
	task use(p)
}