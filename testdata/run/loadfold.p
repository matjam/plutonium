package main

// Loads folded into arithmetic as memory operands must still read memory
// before any store that follows them.

//pu:noinline
func addThenStore(p, q *[4]int64) int64 {
	a := p[1]
	q[1] = 100
	return a + q[2]
}

//pu:noinline
func fsubThenStore(p, q *[4]float64) float64 {
	a := p[0]
	b := p[1]
	q[1] = 7
	return a - b
}

//pu:noinline
func mixed(p *[4]uint32, f *[2]float64) (uint32, float64) {
	x := p[0] + p[1]
	y := p[2] ^ x
	z := f[0]*f[1] + f[0]
	return y - p[3], z / f[1]
}

//pu:noinline
func narrow(b []byte, h []int16) (byte, int16) {
	return b[0] + b[1], h[0] - h[1]
}

func main() {
	var a [4]int64
	a[1], a[2] = 1, 2
	println(addThenStore(&a, &a), a[1])
	var f [4]float64
	f[0], f[1] = 10, 3
	println(int64(fsubThenStore(&f, &f)), int64(f[1]))
	u := [4]uint32{1, 2, 0xff, 4}
	g := [2]float64{1.5, 4}
	x, y := mixed(&u, &g)
	println(x, int64(y*1000))
	c, d := narrow([]byte{200, 100}, []int16{-5, 30000})
	println(c, d)
}
