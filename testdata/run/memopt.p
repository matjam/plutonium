package main

// Memory operations that look redundant but are not, and some that are.

type P struct {
	x, y, m float64
	n       int64
}

var ps [8]P
var g [4]int64

//pu:noinline
func alias(p, q *[4]int64) int64 {
	a := p[1]
	q[1] = a + 5 // p and q may be the same array
	return p[1] + a
}

//pu:noinline
func storeBack(i, j int) int64 {
	x := g[i]
	g[j] = 7
	g[i] = x // not redundant when i == j
	return g[0] + g[1] + g[2]
}

//pu:noinline
func overwrite(p *P) P {
	p.x = 1
	c := *p // reads p.x == 1 before the next store
	p.x = 2
	p.x = 3
	return c
}

//pu:noinline
func touch(p *P) float64 { return p.y }

//pu:noinline
func deadAcrossCall(p *P) float64 {
	p.y = 4
	t := touch(p) // reads p.y
	p.y = 5
	return t + p.y
}

//pu:noinline
func forward(i int) float64 {
	ps[i].x = 1.5
	ps[i+1].x = 2.5 // a different element
	ps[i].y = ps[i].x * 2
	return ps[i].y + ps[i+1].x
}

//pu:noinline
func update(n int) {
	for i := 0; i < n; i++ {
		p := ps[i]
		p.x += p.y * p.m
		p.n++
		ps[i] = p // p.y and p.m are stored back unchanged
	}
}

//pu:noinline
func slots(k int64) int64 {
	var a, b [2]int64
	a[0] = k
	b[0] = k + 1
	a[1] = b[0]
	b[0] = 0
	return a[0] + a[1] + b[0] + b[1]
}

func main() {
	println(alias(&g, &g))
	var h [4]int64
	println(alias(&g, &h), h[1])
	g = [4]int64{1, 2, 3, 4}
	println(storeBack(1, 1), storeBack(0, 2))
	var p P
	c := overwrite(&p)
	println(int64(c.x), int64(p.x))
	println(int64(deadAcrossCall(&p)))
	println(int64(forward(2)))
	for i := range ps {
		ps[i] = P{x: float64(i), y: 0.5, m: float64(i) + 1, n: int64(i)}
	}
	update(5)
	update(8)
	for i := range ps {
		println(int64(ps[i].x*100), int64(ps[i].y*100), int64(ps[i].m), ps[i].n)
	}
	println(slots(10))
}
