package main

// Float constants as instruction operands, and negation through a sign
// mask, at both widths.

func milli(f float64) int64 { return int64(f * 1000) }

func neg64(x float64) float64 { return -x }

func neg32(x float32) float32 { return -x }

func ops64(x float64) (float64, float64, float64, float64) {
	return x + 0.25, x - 1.5, x * 2.5, x / 0.125
}

func ops32(x float32) (float32, float32, float32, float32) {
	return x + 0.25, x - 1.5, x * 2.5, x / 0.125
}

func poly32(x float32) float32 { return 1.5*x*x - 0.75*x + 3.25 }

func main() {
	a, b, c, d := ops64(3)
	println("ops64", milli(a), milli(b), milli(c), milli(d))
	e, f, g, h := ops32(3)
	println("ops32", milli(float64(e)), milli(float64(f)), milli(float64(g)), milli(float64(h)))
	println("poly32", milli(float64(poly32(2))), milli(float64(poly32(-1.5))))
	println("neg64", milli(neg64(2.5)), milli(neg64(-0.125)))
	println("neg32", milli(float64(neg32(2.5))), milli(float64(neg32(-0.125))))
	var z64 float64
	var z32 float32
	println("negzero64", 1/neg64(z64) < 0, 1/neg64(neg64(z64)) > 0)
	println("negzero32", 1/neg32(z32) < 0, 1/neg32(neg32(z32)) > 0)
	sum := float32(0)
	for i := 0; i < 10; i++ {
		sum = sum*0.5 + 1
		sum = -sum
	}
	println("loop32", milli(float64(sum)))
}
