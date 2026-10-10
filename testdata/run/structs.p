package main

type Point struct{ X, Y int }

func (p Point) Add(q Point) Point { return Point{p.X + q.X, p.Y + q.Y} }
func (p *Point) Scale(k int)      { p.X *= k; p.Y *= k }
func (p Point) Dot(q Point) int   { return p.X*q.X + p.Y*q.Y }

type Mixed struct {
	a uint8
	b int64
	c uint16
	d bool
	e int32
}

type Big struct {
	a, b, c, d, e, f, g int
}

func makeBig(n int) Big { return Big{n, n + 1, n + 2, n + 3, n + 4, n + 5, n + 6} }

func sumBig(b Big) int { return b.a + b.b + b.c + b.d + b.e + b.f + b.g }

func mutate(b Big) Big {
	b.a = 1000
	return b
}

type Base struct{ id int }

func (b Base) ID() int      { return b.id }
func (b *Base) SetID(i int) { b.id = i }

type Derived struct {
	Base
	name string
}

type Node struct {
	val  int
	next *Node
}

type Vec3 struct{ x, y, z float64 }

func (a Vec3) Add(b Vec3) Vec3      { return Vec3{a.x + b.x, a.y + b.y, a.z + b.z} }
func (a Vec3) Scale(k float64) Vec3 { return Vec3{a.x * k, a.y * k, a.z * k} }

func main() {
	p := Point{1, 2}
	q := p.Add(Point{3, 4})
	q.Scale(10)
	println("point", q.X, q.Y, p.Dot(q))
	r := q
	r.X = 99
	println("copy", q.X, r.X)
	println("eq", p == Point{1, 2}, p == q)

	var m Mixed
	m.a, m.b, m.c, m.d, m.e = 200, -5, 60000, true, -77
	m2 := m
	m2.c++
	println("mixed", m.a, m.b, m.c, m.d, m.e, m2.c, m == m2)

	b := makeBig(10)
	b2 := mutate(b)
	println("big", sumBig(b), sumBig(b2), b.a, b2.a)

	var arr [4]Point
	for i := range arr {
		arr[i] = Point{i, i * i}
	}
	total := 0
	for _, pt := range arr {
		total += pt.X + pt.Y
	}
	println("array of structs", total)

	d := Derived{Base{7}, "seven"}
	d.SetID(8)
	println("embedded", d.ID(), d.id, d.name)

	nodes := [5]Node{}
	for i := 0; i < 4; i++ {
		nodes[i] = Node{val: i * 10, next: &nodes[i+1]}
	}
	nodes[4] = Node{val: 40}
	s := 0
	for n := &nodes[0]; n != nil; n = n.next {
		s += n.val
	}
	println("list", s)

	f := p.Add
	println("method value", f(Point{5, 5}).X)
	g := Point.Dot
	println("method expr", g(p, Point{2, 3}))

	v := Vec3{1, 2, 3}.Add(Vec3{0.5, 0.25, 0.125}).Scale(2)
	println("vec", int(v.x*1000), int(v.y*1000), int(v.z*1000))
}
