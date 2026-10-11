package main

// append, recover, complex, real and imag, and complex64 and complex128, are
// not predeclared in Plutonium (language spec sections 10.3 and 13.1): a
// program may declare them as its own. Go predeclares them, and the
// program's declarations hide those, so it means the same in both.

type complex128 struct{ re, im float64 }

type complex64 = complex128

func complex(re, im float64) complex128 { return complex128{re, im} }

func real(c complex128) float64 { return c.re }

func imag(c complex128) float64 { return c.im }

type stack struct {
	items [8]int
	n     int
}

func append(s *stack, x int) *stack {
	s.items[s.n] = x
	s.n++
	return s
}

var recovered int

func recover() int {
	recovered++
	return recovered
}

func main() {
	var z complex64 = complex(1.5, -2)
	println(real(z), imag(z))

	var s stack
	append(append(&s, 4), 5)
	println(s.n, s.items[0], s.items[1])

	println(recover(), recover())

	// Local declarations hide them too.
	{
		append := 10
		real := append * 2
		println(append, real)
	}
}
