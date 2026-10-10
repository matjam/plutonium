package main

// Exact float identities: x*1, x*-1, x/-1, a+(-b), (-a)+b, a-(-b), -(-x).
// Checked on ordinary values, signed zeros and infinities.

//pu:noinline
func mulNeg(x float64) float64 { return x * -1 }

//pu:noinline
func negMul(x float64) float64 { return -1 * x }

//pu:noinline
func divNeg(x float64) float64 { return x / -1.0 }

//pu:noinline
func addNeg(a, b float64) float64 { return a + -b }

//pu:noinline
func negAdd(a, b float64) float64 { return -a + b }

//pu:noinline
func subNeg(a, b float64) float64 { return a - -b }

//pu:noinline
func negNeg(x float64) float64 { return -(-x) }

//pu:noinline
func mulNeg32(x float32) float32 { return x * -1 }

//pu:noinline
func addNeg32(a, b float32) float32 { return a + -b }

// show prints f in a form Go and Plutonium agree on, keeping the sign of zero.
func show(f float64) {
	switch {
	case f != f:
		print(" nan")
	case f > 1e308:
		print(" +inf")
	case f < -1e308:
		print(" -inf")
	case f == 0 && 1/f < 0:
		print(" -0")
	case f == 0:
		print(" +0")
	default:
		print(" ", int64(f*1000))
	}
}

func main() {
	var zero float64
	negz := -zero
	inf := 1 / zero
	xs := []float64{2.5, -0.75, zero, negz, inf, -inf}
	for _, x := range xs {
		show(mulNeg(x))
		show(negMul(x))
		show(divNeg(x))
		show(negNeg(x))
		show(float64(mulNeg32(float32(x))))
		println()
	}
	for _, a := range xs {
		for _, b := range xs {
			show(addNeg(a, b))
			show(negAdd(a, b))
			show(subNeg(a, b))
			show(float64(addNeg32(float32(a), float32(b))))
		}
		println()
	}
}
