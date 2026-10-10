package main

// Comparison of arrays longer than eight elements whose equality is not
// equality of their bytes: structs with padding, floats (where -0 equals
// +0 and NaN equals nothing) and nested arrays of such. Found by
// tools/difftest: these were rejected as not supported.

type padded struct {
	a int8
	b int64
	c bool
}

type withFloat struct {
	x float64
	n int16
}

var gz int32

//pu:noinline
func eqPadded(x, y [12]padded) bool { return x == y }

//pu:noinline
func eqFloat(x, y *[9]withFloat) bool { return *x == *y }

//pu:noinline
func neFloats(x, y [10]float32) bool { return x != y }

func main() {
	var p, q [12]padded
	println(eqPadded(p, q))
	p[11].c = true
	println(eqPadded(p, q))
	q[11].c = true
	q[3] = padded{a: -1, b: 1 << 40}
	p[3] = q[3]
	println(eqPadded(p, q), p == q, p != q)

	z := float64(gz)
	var f, g [9]withFloat
	f[8].x = z
	g[8].x = -z
	println(eqFloat(&f, &g)) // -0 == +0
	f[0].x = z / z
	g[0].x = f[0].x
	println(eqFloat(&f, &g), eqFloat(&f, &f)) // NaN != NaN
	f[0].x, g[0].x = 1.5, 1.5
	f[4].n = 7
	println(eqFloat(&f, &g))
	g[4].n = 7
	println(eqFloat(&f, &g))

	var a, b [10]float32
	a[9] = float32(z)
	b[9] = -float32(z)
	println(neFloats(a, b), a == b)
	b[2] = 1
	println(neFloats(a, b), a == b)

	var m, n [3][9]padded
	n[2][8].b = 5
	println(m == n)
	m[2][8].b = 5
	println(m == n)
}
