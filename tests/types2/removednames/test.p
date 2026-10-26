package main

type complex128 struct{ re, im float64 }

func complex(re, im float64) complex128 { return complex128{re, im} }
func real(c complex128) float64          { return c.re }
func imag(c complex128) float64          { return c.im }

type complex64 = complex128

func append(s []int, x int) int { return len(s) + x }

func recover() bool { return false }

func f() float64 {
	var z complex64 = complex(1, 2)
	n := append([]int{1}, 2)
	if recover() {
		return 0
	}
	return real(z) + imag(z) + float64(n)
}

func g() int {
	append := 3
	real := append * 2
	return real
}
