// The indexing interface (language spec section 6.7): index expressions,
// index assignment, comma-ok, compound assignment and len on library
// containers and on types of the program's own, through their Index,
// SetIndex, IndexOk and Len methods, directly, through pointers, through
// interfaces and in generic code.
package main

import (
	"pluto/container"
	"pluto/hash"
	"pluto/vec"
)

// grid is a container of the program's own with value receivers: a fixed
// 3x3 board indexed by a pair.
type pos struct{ r, c int }

type grid struct {
	cells [9]int
}

func (g grid) Index(p pos) int { return g.cells[p.r*3+p.c] }
func (g *grid) SetIndex(p pos, v int) {
	g.cells[p.r*3+p.c] = v
}
func (g grid) Len() int { return len(g.cells) }

// board embeds grid, so its indexing methods are promoted.
type board struct {
	name string
	grid
}

type point struct{ x, y int }

func (p point) sum() int { return p.x + p.y }

var calls int

func key(k string) string {
	calls++
	return k
}

// count adds one for each key, in generic code over any Indexable.
func count[M container.Indexable[K, int], K any](m M, keys []K) {
	for _, k := range keys {
		m[k] += 1
	}
}

// total sums a container of ints through a constraint of its own.
func total[C interface {
	Index(i int) int
	Len() int
}](c C) int {
	t := 0
	for i := 0; i < len(c); i++ {
		t += c[i]
	}
	return t
}

// lookup uses the comma-ok form in generic code.
func lookup[M container.Lookup[K, V], K, V any](m M, k K) (V, bool) {
	v, ok := m[k]
	return v, ok
}

func first(r container.Reader[int, string]) string { return r[0] }

func main() {
	m := hash.NewMap[string, int](4)
	defer m.Free()

	println("missing:", m["a"], len(m))
	m["a"] = 1
	m["b"] = 2
	m["a"] += 10
	m["b"]++
	m["c"] -= 3
	println("a b c:", m["a"], m["b"], m["c"], len(m))

	v, ok := m["a"]
	println("comma-ok a:", v, ok)
	v, ok = m["z"]
	println("comma-ok z:", v, ok)
	if n, ok := m["b"]; ok {
		println("if b:", n)
	}
	var c, okc = m["c"]
	println("var c:", c, okc)

	// x and k are evaluated once by a compound assignment.
	m[key("a")] *= 2
	m[key("d")]++
	println("once:", calls, m["a"], m["d"])

	// Through a pointer.
	p := &m
	p["e"] = 5
	p["e"] <<= 2
	println("pointer:", p["e"], len(p))

	// Generic code over the map, through its pointer.
	words := []string{"x", "y", "x", "x"}
	count(&m, words)
	println("count:", m["x"], m["y"])
	if n, ok := lookup(&m, "y"); ok {
		println("lookup y:", n)
	}
	_, ok = lookup(&m, "nope")
	println("lookup nope:", ok)

	// Vec.
	vs := vec.New[int]()
	defer vs.Free()
	for i := 0; i < 5; i++ {
		vs.Push(i * i)
	}
	vs[0] = 100
	vs[1] *= 7
	vs[2], vs[3] = vs[3], vs[2]
	print("vec:")
	for i := 0; i < len(vs); i++ {
		print(" ", vs[i])
	}
	println()
	println("total:", total(&vs))

	pts := vec.New[point]()
	defer pts.Free()
	pts.Push(point{1, 2})
	pts.Push(point{3, 4})
	q := pts[1]
	q.x = 30
	pts[1] = q
	println("points:", pts[0].x, pts[1].x, pts[1].sum())

	names := vec.New[string]()
	defer names.Free()
	names.Push("zero")
	println("reader:", first(&names))

	// The program's own container, with value receivers and promotion.
	var g grid
	g[pos{1, 1}] = 5
	g[pos{2, 0}] += 7
	println("grid:", g[pos{1, 1}], g[pos{2, 0}], g[pos{0, 0}], len(g))
	b := board{name: "b"}
	b[pos{0, 2}] = 9
	println("board:", b.name, b[pos{0, 2}], len(b))
	gp := &g
	gp[pos{0, 0}] = -1
	println("grid pointer:", g[pos{0, 0}])
}
