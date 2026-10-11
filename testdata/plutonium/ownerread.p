package main

// Only comparisons apply to an owner; a conversion to a type that is not an
// owner reads its value and moves nothing (spec sections 11.18 and 11.24).

type Fd owner int32

func (f *Fd) Drop() {
	if *f != 0 {
		println("close", int32(*f))
	}
}

func open(n int32) Fd { return Fd(n) }

func next(f *Fd) int32 { return int32(*f) + 1 }

func main() {
	f := open(3)
	g := open(4)
	println(next(&f), int32(f)*10, f == 3, f != g, f < g)
	n := int64(g) << 2
	println(n, uint8(f)+uint8(g))
	// f and g still hold their descriptors, and are dropped once each.
}
