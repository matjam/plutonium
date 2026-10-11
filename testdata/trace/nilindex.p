package main

// An element of a large array reached through a pointer can be far from
// address zero: a store to it through a nil pointer is checked explicitly
// and panics instead of writing to whatever memory is there.

type grid struct {
	cells [512][512]int32
}

func (g *grid) set(x, y int, v int32) {
	g.cells[y][x] = v
}

func fill(p *[8192]int, i int) {
	p[i] = i
}

func main() {
	g := &grid{}
	g.set(3, 400, 7)
	println(g.cells[400][3])
	var a [8192]int
	fill(&a, 8000)
	println(a[8000])
	var none *grid
	none.set(3, 400, 7)
}
