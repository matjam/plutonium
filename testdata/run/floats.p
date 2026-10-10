package main

func milli(f float64) int64 { return int64(f * 1000) }

func sqrtNewton(x float64) float64 {
	z := x / 2
	for i := 0; i < 20; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

func poly(x float64) float64 { return 3*x*x*x - 2*x*x + x - 7 }

type Body struct{ x, y, vx, vy, m float64 }

func step(bs []Body, dt float64) {
	for i := range bs {
		bs[i].x += bs[i].vx * dt
		bs[i].y += bs[i].vy * dt
	}
}

func main() {
	a, b := 1.5, -2.25
	println("ops", milli(a+b), milli(a-b), milli(a*b), milli(a/b), milli(-a))
	println("cmp", a < b, a > b, a == 1.5, a != b, a <= 1.5, b >= -2.25)
	var f32 float32 = 1.25
	f32 *= 3
	println("f32", int64(f32*100), int64(float64(f32)*1000))
	println("sqrt2", milli(sqrtNewton(2)))
	println("poly", milli(poly(1.5)), milli(poly(-0.5)))
	i := int64(-7)
	u := uint64(18446744073709551000)
	println("int2float", milli(float64(i)/2), int64(float64(u)/1e12))
	big := 1e19
	println("float2uint", uint64(big), uint32(3.99e9), int32(-1.9e9))
	zero := 0.0
	nan := zero / zero
	println("nan", nan == nan, nan != nan, nan < 1, nan > 1)
	inf := 1 / zero
	println("inf", inf > 1e308, -inf < -1e308)
	bs := []Body{{0, 0, 1, 2, 1}, {1, 1, -1, 0.5, 2}}
	for k := 0; k < 100; k++ {
		step(bs, 0.01)
	}
	println("bodies", milli(bs[0].x), milli(bs[0].y), milli(bs[1].x), milli(bs[1].y))
	println("minmax", milli(min(a, b)), milli(max(a, b)), min(3, 1, 2), max(int8(-1), 5))
	var acc float64
	for k := 1; k <= 1000; k++ {
		acc += 1 / float64(k*k)
	}
	println("basel", milli(acc*1000))
}
