package main

// recover, complex, real, imag, complex64 and complex128 are not
// predeclared (spec sections 10.3 and 13.1). A use without a declaration of
// the program's own is an error that says what to write instead.

func risky() {
	defer func() {
		if r := recover(); r != nil {
			println("recovered")
		}
	}()
	panic("boom")
}

func magnitude(z complex128) float64 {
	return real(z)*real(z) + imag(z)*imag(z)
}

func main() {
	risky()
	var z complex64
	_ = z
	println(magnitude(complex(3, 4)))
}
