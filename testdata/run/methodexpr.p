package main

// Method expressions: T.M and (*T).M, for value and pointer methods.

type K uint8

func (k K) Name() string {
	if k == 2 {
		return "two"
	}
	return "other"
}

func (k K) Add(n int) int { return int(k) + n }

func (k *K) Inc() { *k++ }

type P struct {
	x, y int
}

func (p P) Sum(scale int) int { return (p.x + p.y) * scale }

func (p *P) Scale(n int) { p.x *= n; p.y *= n }

func main() {
	k := K(2)
	byValue := K.Name
	byPtr := (*K).Name
	println(byValue(k), byPtr(&k))
	add := (*K).Add
	println(add(&k, 40))
	inc := (*K).Inc
	inc(&k)
	println(byPtr(&k), int(k))

	p := P{1, 2}
	sum := (*P).Sum
	scale := (*P).Scale
	scale(&p, 10)
	println(sum(&p, 2), P.Sum(p, 1))
}
