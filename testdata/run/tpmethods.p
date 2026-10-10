// Methods of a constraint called on values of a type parameter: value and
// pointer receivers, promoted methods, generic receiver types, generic
// constraints, method values, core-type constraints with methods, the
// *T pattern, and calls inside methods of generic types.
package main

type Stringer interface{ String() string }

type S struct{ name string }

func (s S) String() string { return s.name }

type P struct{ n int }

func (p *P) String() string {
	p.n++
	if p.n == 1 {
		return "P1"
	}
	return "Pn"
}

type I int

func (i I) String() string {
	if i < 0 {
		return "neg"
	}
	return "I"
}

type Inner struct{ v int }

func (Inner) String() string { return "inner" }

type Outer struct {
	x int
	Inner
}

type PInner struct{ v int }

func (p *PInner) String() string {
	p.v += 10
	if p.v > 10 {
		return "pinner>10"
	}
	return "pinner"
}

type Outer2 struct {
	pad [3]int
	*PInner
}

type Outer3 struct {
	a int
	PInner
}

type Vec[E any] struct {
	items [4]E
	n     int
}

func (v Vec[E]) String() string {
	if v.n == 0 {
		return "empty"
	}
	return "vec"
}

func (v *Vec[E]) Len() int { return v.n }

func f[T Stringer](x T) string { return x.String() }

func twice[T Stringer](x T) (string, string) { return x.String(), x.String() }

func mv[T Stringer](x T) string {
	m := x.String
	return m()
}

type Getter[U any] interface{ Get() U }

type Box[U any] struct{ v U }

func (b Box[U]) Get() U { return b.v }

type IntGet int

func (g IntGet) Get() int { return int(g) * 2 }

func get[T Getter[U], U any](x T) U { return x.Get() }

type Lener interface{ Len() int }

func lenOf[T Lener](x T) int { return x.Len() }

type Coll[T Stringer] struct {
	items [3]T
	n     int
}

func (c *Coll[T]) Add(x T) { c.items[c.n] = x; c.n++ }

func (c *Coll[T]) Dump() {
	for i := 0; i < c.n; i++ {
		println("dump", c.items[i].String())
	}
	if c.n > 0 {
		m := c.items[0].String
		println("first", m())
	}
}

type IntStringer interface {
	~int
	String() string
}

func both[T IntStringer](x T) int {
	println(x.String())
	return int(x) + 1
}

type Setter[T any] interface {
	*T
	Set(int)
	String() string
}

type Cell struct{ v int }

func (c *Cell) Set(v int) { c.v = v }
func (c *Cell) String() string {
	if c.v > 5 {
		return "big"
	}
	return "small"
}

func setAll[T any, PT Setter[T]](xs []T, v int) {
	for i := range xs {
		p := PT(&xs[i])
		p.Set(v + i)
		println(p.String())
	}
}

type Ider interface{ ID() int }

type Node struct{ id int }

func (n Node) ID() int { return n.id }

func ids[T Ider](xs []T) int {
	t := 0
	for _, x := range xs {
		t += x.ID()
	}
	return t
}

func viaIface[T Stringer](x T) string {
	var s Stringer = x
	return s.String()
}

type Multi interface {
	A() int
	B(int) int
}

type MM struct{ base int }

func (m MM) A() int       { return m.base }
func (m *MM) B(x int) int { m.base += x; return m.base }

func useMulti[T Multi](x T) int { return x.A() + x.B(3) + x.A() }

func main() {
	println(f(S{"a"}))
	p := &P{}
	println(f(p), f(p), p.n)
	println(f(I(-1)), f(I(3)))
	println(f(Outer{1, Inner{2}}))
	println(f(&Outer{}))
	o2 := Outer2{PInner: &PInner{}}
	println(f(o2), o2.PInner.v)
	println(f(&o2), o2.PInner.v)
	o3 := &Outer3{}
	println(f(o3), o3.v)
	println(f(Vec[int]{}), f(&Vec[string]{n: 1}))
	println(f[*S](&S{"ptr"}))
	a, b := twice(&P{})
	println(a, b)
	println(mv(S{"mv"}), mv(&P{}), mv(I(1)))
	println(get[Box[string]](Box[string]{"boxed"}), get[IntGet](IntGet(21)))
	println(lenOf(&Vec[int]{n: 3}))
	var c Coll[S]
	c.Add(S{"x"})
	c.Add(S{"y"})
	c.Dump()
	var cp Coll[*P]
	cp.Add(&P{})
	cp.Dump()
	println(both(I(4)))
	var cellArr [3]Cell
	cells := cellArr[:]
	setAll(cells, 4)
	println(cells[0].v, cells[2].v)
	println(ids([]Node{{1}, {2}, {3}}))
	println(viaIface(S{"if"}), viaIface(&P{n: 1}))
	println(useMulti(&MM{base: 1}))
}
