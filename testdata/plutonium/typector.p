package main

import "pluto/fmt"

// Type constructors with constant arguments used in expressions: composite
// literals and conversions, not only declarations.

type Queue = ring[int, 8]

func sum(v uint64) uint64 { return v + 1 }

func main() {
	r := ring[int, 8]{}
	r.Push(3)
	r.Push(4)
	fmt.Printf("len=%d cap=%d\n", r.Len(), r.Cap())
	r = ring[int, 8]{}
	fmt.Printf("after reset len=%d\n", r.Len())

	q := Queue{}
	q.Push(1)
	fmt.Printf("q len=%d\n", q.Len())

	var x uint32 = 0xdeadbeef
	a := aligned[uint32, 4](x)
	fmt.Printf("a=%x\n", a)
	fmt.Printf("sum=%d\n", sum(aligned[uint64, 8](41)))

	b := aligned[[4]uint8, 16]{1, 2, 3, 4}
	fmt.Printf("b=%d %d\n", b[0], b[3])

	v := volatile[uint8](7)
	fmt.Printf("v=%d\n", v)
}
