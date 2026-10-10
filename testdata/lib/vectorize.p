package main

// Loops the vectoriser runs two iterations at a time, and loops it must
// leave alone, over random and special float64 values. Each result is
// folded into an FNV-1a digest of its bits; the digests match Go compiling
// the same source, so the packed code is bit-identical to scalar code.

import (
	"pluto/fmt"
	"pluto/math"
)

var seed uint64 = 11

func rnd() uint64 {
	seed = seed*6364136223846793005 + 1442695040888963407
	return seed
}

var specials = [...]float64{0, 1, -1, 0.5, 3, 1e308, -1e308, 5e-324, -5e-324, 2.2250738585072014e-308}

// value returns a float64 that is sometimes special: zeros, infinities,
// NaN, denormals, huge and tiny values.
func value() float64 {
	r := rnd()
	switch r % 16 {
	case 0:
		return math.Inf(1)
	case 1:
		return math.Inf(-1)
	case 2:
		return math.NaN()
	case 3:
		return math.Copysign(0, -1)
	case 4, 5:
		return specials[(r>>8)%uint64(len(specials))]
	}
	return math.Float64frombits(r>>2|0x3000000000000000) - 1.5
}

// digest folds x into h. Which NaN an operation on two NaNs returns
// depends on operand order, which compilers choose freely, so every NaN
// counts the same.
func digest(h uint64, x float64) uint64 {
	b := math.Float64bits(x)
	if x != x {
		b = 0x7FF8000000000001
	}
	for i := 0; i < 8; i++ {
		h ^= b & 0xff
		h *= 1099511628211
		b >>= 8
	}
	return h
}

type Vec3 struct{ X, Y, Z float64 }

type Particle struct {
	Pos, Vel Vec3
	W        float64
}

const N = 101

var ps [N]Particle
var a, b, c, d [N]float64

func (v Vec3) Add(u Vec3) Vec3      { return Vec3{v.X + u.X, v.Y + u.Y, v.Z + u.Z} }
func (v Vec3) Scale(k float64) Vec3 { return Vec3{v.X * k, v.Y * k, v.Z * k} }
func (v Vec3) Sub(u Vec3) Vec3      { return Vec3{v.X - u.X, v.Y - u.Y, v.Z - u.Z} }
func (v Vec3) Div(u Vec3) Vec3      { return Vec3{v.X / u.X, v.Y / u.Y, v.Z / u.Z} }
func (v Vec3) Neg() Vec3            { return Vec3{-v.X, -v.Y, -v.Z} }
func (v Vec3) Sqrt() Vec3           { return Vec3{math.Sqrt(v.X), math.Sqrt(v.Y), math.Sqrt(v.Z)} }

func (v Vec3) Cross(u Vec3) Vec3 {
	return Vec3{v.Y*u.Z - v.Z*u.Y, v.Z*u.X - v.X*u.Z, v.X*u.Y - v.Y*u.X}
}

func step(p Particle, f Vec3, k, dt float64) Particle {
	acc := p.Pos.Scale(-k).Add(p.Vel.Cross(f)).Scale(p.W)
	p.Vel = p.Vel.Add(acc.Scale(dt))
	p.Pos = p.Pos.Add(p.Vel.Scale(dt)).Sub(p.Vel.Div(f).Neg())
	return p
}

// An odd trip count leaves one iteration to the scalar loop.

//pu:noinline
func particles(f Vec3, k, dt float64) {
	for i := 0; i < N; i++ {
		ps[i] = step(ps[i], f, k, dt)
	}
}

// The trip count is only known at run time.

//pu:noinline
func particleSlice(s []Particle, f Vec3, k, dt float64) {
	for i := 0; i < len(s); i++ {
		s[i] = step(s[i], f, k, dt)
	}
}

//pu:noinline
func scaleSlice(s []float64, k float64) {
	for i := 0; i < len(s); i++ {
		s[i] = s[i]*k - 0.5
	}
}

//pu:noinline
func roots() {
	for i := 0; i < N; i++ {
		p := &ps[i]
		p.Vel = p.Vel.Sqrt().Add(p.Pos)
	}
}

// Contiguous arrays use 16-byte loads and stores.

//pu:noinline
func axpy(k float64) {
	for i := 0; i < N; i++ {
		c[i] = a[i]*k + b[i]/c[i]
	}
}

//pu:noinline
func mixed(k float64) {
	for i := 0; i < N; i++ {
		d[i] = math.Sqrt(a[i]*a[i]+k) - -b[i]
		c[i] = d[i] * 0.25
	}
}

//pu:noinline
func short(k float64) {
	for i := 0; i < 1; i++ {
		a[i] = a[i]*k + 1
	}
	for i := 0; i < 2; i++ {
		b[i] = b[i]*k + 2
	}
	for i := 0; i < 3; i++ {
		c[i] = c[i]*k + 3
	}
}

type Row [18]float64

var rows [N]Row

// Eighteen live vectors do not fit in sixteen registers: some are
// spilled, sixteen bytes at a time.

//pu:noinline
func spill(k float64) {
	for i := 0; i < N; i++ {
		r := &rows[i]
		x0, x1, x2, x3, x4, x5, x6, x7, x8 := r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8]
		y0, y1, y2, y3, y4, y5, y6, y7, y8 := r[9], r[10], r[11], r[12], r[13], r[14], r[15], r[16], r[17]
		r[0], r[1], r[2], r[3], r[4], r[5], r[6], r[7], r[8] = x0*y8+k, x1*y7-k, x2*y6+k, x3*y5-k, x4*y4+k, x5*y3-k, x6*y2+k, x7*y1-k, x8*y0+k
		r[9], r[10], r[11], r[12], r[13], r[14], r[15], r[16], r[17] = y0/x8, y1/x7, y2/x6, y3/x5, y4/x4, y5/x3, y6/x2, y7/x1, y8/x0
	}
}

// Loops that must stay scalar.

//pu:noinline
func shift() {
	// a[i] reads what the previous iteration wrote.
	for i := 1; i < N; i++ {
		a[i] = a[i-1]*0.5 + a[i]*0.25
	}
}

//pu:noinline
func through(p, q *[N]float64) {
	// p and q may be the same array.
	for i := 0; i < N; i++ {
		q[i] = p[i]*2 + 1
		p[i] = q[i] * 3
	}
}

//pu:noinline
func overlap(s, t []float64) {
	// t may start one element into s.
	for i := 0; i < len(s); i++ {
		s[i] = s[i]*0.5 + 1
		if i < len(t) {
			t[i] = t[i] * 3
		}
	}
}

func fill() {
	for i := range ps {
		ps[i] = Particle{Vec3{value(), value(), value()}, Vec3{value(), value(), value()}, value()}
		a[i], b[i], c[i], d[i] = value(), value(), value(), value()
		for j := range rows[i] {
			rows[i][j] = value()
		}
	}
}

func sum() uint64 {
	h := uint64(14695981039346656037)
	for i := range ps {
		p := ps[i]
		for _, x := range [...]float64{p.Pos.X, p.Pos.Y, p.Pos.Z, p.Vel.X, p.Vel.Y, p.Vel.Z, p.W, a[i], b[i], c[i], d[i]} {
			h = digest(h, x)
		}
		for _, x := range rows[i] {
			h = digest(h, x)
		}
	}
	return h
}

func main() {
	fill()
	for _, n := range [...]int{0, 1, 2, 3, 4, 17, N} {
		particleSlice(ps[:n], Vec3{value(), value(), 0.5}, value(), 0.001)
		fmt.Printf("particleSlice %d %016x\n", n, sum())
		scaleSlice(d[N-n:], value())
		fmt.Printf("scaleSlice %d %016x\n", n, sum())
		overlap(a[:n], a[1:])
		fmt.Printf("overlap %d %016x\n", n, sum())
	}
	for range 3 {
		particles(Vec3{value(), value(), 0.5}, value(), 0.001)
		fmt.Printf("particles %016x\n", sum())
		roots()
		fmt.Printf("roots %016x\n", sum())
		axpy(value())
		fmt.Printf("axpy %016x\n", sum())
		mixed(value())
		fmt.Printf("mixed %016x\n", sum())
		short(value())
		fmt.Printf("short %016x\n", sum())
		spill(value())
		fmt.Printf("spill %016x\n", sum())
		shift()
		fmt.Printf("shift %016x\n", sum())
		through(&a, &a)
		through(&b, &c)
		fmt.Printf("through %016x\n", sum())
		fill()
	}
}
