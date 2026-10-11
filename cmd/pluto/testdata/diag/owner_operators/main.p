package main

// Only comparisons apply to an owner (spec section 11.18); a conversion to
// the underlying type reads the value without moving it.

type Fd owner int32

func (f *Fd) Drop() {}

func next(f *Fd) int32 {
	if *f == 0 || *f != 3 { // comparisons apply
		return int32(*f) + 1 // the conversion reads the number
	}
	return *f + 1
}

func main() {
	f := Fd(3)
	g := -f
	f++
	f <<= 1
	_ = 2 * f
	_ = g
	println(next(&f))
}
