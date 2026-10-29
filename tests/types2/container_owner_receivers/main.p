// x[k] and len(x) on an owner type call its Index, SetIndex, IndexOk and
// Len methods with pointer receivers, which borrow the container: it is
// used again after each and dropped once, at the end (language spec
// sections 11.11 and 13.6). A type that is not an owner may declare them
// on value receivers.
package main

type Bag owner struct {
	cells [4]int
}

func (b *Bag) Drop() { println("drop bag", b.cells[0]) }

func (b *Bag) Index(k int) int           { return b.cells[k] }
func (b *Bag) SetIndex(k int, v int)     { b.cells[k] = v }
func (b *Bag) IndexOk(k int) (int, bool) { return b.cells[k], k > 0 }
func (b *Bag) Len() int                  { return len(b.cells) }

type Grid struct {
	cells [3]int
}

func (g Grid) Index(k int) int           { return g.cells[k] * 10 }
func (g Grid) IndexOk(k int) (int, bool) { return g.cells[k], true }
func (g Grid) Len() int                  { return len(g.cells) }

func fill(p *Bag) {
	for i := 0; i < len(p); i++ {
		p[i] = i + 1
	}
}

func main() {
	b := Bag{}
	fill(&b)
	b[0] += 6
	v, ok := b[0]
	println(b[0], b[3], v, ok, len(b))
	v, ok = b[2]
	println(v, ok, len(b))

	g := Grid{cells: [3]int{1, 2, 3}}
	w, ok2 := g[1]
	println(g[0], g[2], w, ok2, len(g))
	println(len(g), g.cells[0])
}
