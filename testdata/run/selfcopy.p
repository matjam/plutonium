package main

// Copies from one part of a local aggregate to another part of the same
// aggregate. Scalar replacement used to split such a variable one end of
// the copy at a time, so the copy read the old, unsplit variable and
// produced zeros at -O2. Found by tools/difftest.

type S struct {
	a int
	b [3]int8
}

type P struct {
	x, y int32
}

type Pair struct {
	l, r S
}

var g int = -4314977811160330815

func elems() int {
	arr := [2]S{{a: 1}, {a: 2}}
	arr[1] = arr[0]
	return arr[1].a
}

func chain() int32 {
	var ps [3]P
	ps[0] = P{5, 6}
	ps[2] = ps[0]
	ps[1] = ps[2]
	return ps[1].x + ps[2].y
}

func fields() (int, int8) {
	var p Pair
	p.l.a = 9
	p.l.b[2] = -3
	p.r = p.l
	p.l.a = 1
	return p.r.a + p.l.a, p.r.b[2]
}

func viaPointer() int {
	arr := [2]S{{a: 1}, {a: 2}}
	q := &arr[1]
	arr[1] = arr[0]
	return q.a
}

func global() int {
	var agg [3]S
	agg[0].a = g
	agg[2] = agg[0]
	return agg[2].a
}

func nested() int32 {
	var m [2][2]P
	m[0][1] = P{7, 8}
	m[1] = m[0]
	m[0][0] = m[1][1]
	m[1][1].y = 100
	return m[0][0].y*1000 + m[1][1].y + m[1][0].x
}

func self() int {
	s := S{a: 4}
	s = s
	arr := [2]S{{a: 5}, {a: 6}}
	arr[0] = arr[0]
	return s.a*10 + arr[0].a
}

func main() {
	println(elems(), chain(), viaPointer(), global(), nested(), self())
	println(fields())
}
