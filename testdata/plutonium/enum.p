package main

import "pluto/fmt"

// Enum types: members, String, switches, conversions, iteration, tables,
// interfaces, generics and packed fields.

type Color enum {
	Red
	Green
	Blue
}

// A sparse enum with an explicit underlying type: its String uses a table
// with holes.
type Errno enum uint16 {
	OK = 0
	EPERM
	ENOENT
	EIO = 5
}

// Signed, because a member is negative.
type Order enum {
	Less = -1
	Equal
	Greater
}

// Values far apart: String compares instead of indexing a table, and range
// reads the values from a table.
type Size enum uint64 {
	Zero
	Huge = 1 << 40
	Mid  = 1000
}

// An enum with its own String method.
type Level enum {
	Low
	High
}

func (l Level) String() string {
	if l == Level.High {
		return "HIGH"
	}
	return "low"
}

// Next is a method on an enum type.
func (c Color) Next() Color {
	n, _ := Color((int(c) + 1) % len(Color))
	return n
}

var names = [len(Color)]string{Color.Red: "red", Color.Green: "green", Color.Blue: "blue"}

func warmth(c Color) string {
	switch c {
	case Red:
		return "warm"
	case Color.Green, Blue:
		return "cool"
	}
	return "invalid"
}

func compare(a, b int) Order {
	switch {
	case a < b:
		return Order.Less
	case a > b:
		return Order.Greater
	}
	return Order.Equal
}

func Max[T interface{ ~uint8 }](a, b T) T {
	if a > b {
		return a
	}
	return b
}

type Reg packed uint16 {
	color Color          // 2 bits
	order Order          // 2 bits, signed
	err   bits[Errno, 6] // explicit width
	_     bits[uint, 6]
}

type Pixel struct {
	c Color
	o Order
}

func describe(v any) {
	switch x := v.(type) {
	case Color:
		fmt.Printf("Color %s; ", x.String())
	case Errno:
		fmt.Printf("Errno %v; ", x)
	default:
		fmt.Printf("other; ")
	}
}

// Two local enums with the same name: their String methods, descriptors
// and itabs must not be confused.
func local() string {
	type Dir enum { North; East; South; West }
	d := Dir.South
	fmt.Printf("%v ", d)
	name := d.String
	return name()
}

func local2() string {
	type Dir enum { Up; Down }
	fmt.Printf("%v ", Dir.Down)
	name := Dir.Down.String
	return name()
}

func main() {
	// Members, String and fmt.
	c := Color.Green
	fmt.Printf("%v %s %d %v\n", c, Color.Blue, c, Color.Red)
	fmt.Printf("%v %v %v %v\n", Errno.OK, Errno.EPERM, Errno.ENOENT, Errno.EIO)
	fmt.Printf("%v %v %v %d\n", Order.Less, Order.Equal, Order.Greater, Order.Less)
	fmt.Printf("%v %v %v\n", Size.Zero, Size.Mid, Size.Huge)
	fmt.Printf("%v %v\n", Level.Low, Level.High)
	fmt.Printf("%s %s\n", names[c], names[Color.Blue])
	fmt.Printf("len %d %d %d\n", len(Color), len(Errno), len(Size))

	// Zero values are the zero member.
	var z Color
	var p Pixel
	var arr [2]Errno
	fmt.Printf("zero %v %v %v %v\n", z, p.c, p.o, arr[1])

	// Switches with bare and qualified names; ordering.
	for c := range Color {
		fmt.Printf("%v is %s, next %v, less than Blue %t\n", c, warmth(c), c.Next(), c < Color.Blue)
	}
	fmt.Printf("compare %v %v %v\n", compare(1, 2), compare(2, 2), compare(3, 2))

	// Checked conversions.
	for _, n := range []int{0, 2, 3, -1, 256 + 1} {
		col, ok := Color(n)
		fmt.Printf("Color(%d) = %v %t\n", n, col, ok)
	}
	for _, n := range []uint64{0, 1, 2, 3, 4, 5, 6, 1000, 1 << 40} {
		e, ok := Errno(n)
		var s Size
		s, ok2 := Size(n)
		fmt.Printf("Errno(%d) = %v %t, Size = %v %t\n", n, e, ok, s, ok2)
	}
	for _, n := range []int8{-2, -1, 0, 1, 2} {
		o, ok := Order(n)
		fmt.Printf("Order(%d) = %v %t\n", n, o, ok)
	}
	var o2 Order
	var ok bool
	o2, ok = Order(c) // from another enum
	fmt.Printf("Order(c) = %v %t; constant Color(2) = %v\n", o2, ok, Color(2))
	fmt.Printf("ints %d %d %d\n", int(Color.Blue), uint16(Errno.EIO), int64(Order.Less))

	// Iteration over sparse and signed enums.
	for e := range Errno {
		fmt.Printf("%v=%d ", e, e)
	}
	for o := range Order {
		fmt.Printf("%v=%d ", o, o)
	}
	for s := range Size {
		fmt.Printf("%v=%d ", s, s)
	}
	count := 0
	for range Color {
		count++
	}
	fmt.Printf("count %d\n", count)

	// Interfaces and generics.
	var v any = Color.Blue
	describe(v)
	describe(Errno.EIO)
	describe(7)
	fmt.Printf("end\n")
	fmt.Printf("equal %t %t\n", v == any(Color.Blue), v == any(Color.Red))
	var str fmt.Stringer = Order.Greater
	fmt.Printf("stringer %s\n", str.String())
	fmt.Printf("max %v\n", Max(Color.Green, Color.Red))
	f := Color.Red.String
	byPtr := (*Color).String
	fmt.Printf("method value %s, expression %s %s\n", f(), Color.String(Color.Green), byPtr(&c))

	// Packed fields.
	var r Reg
	r.color = Color.Blue
	r.order = Order.Less
	r.err = Errno.EIO
	fmt.Printf("reg %#x: %v %v %v\n", uint16(r), r.color, r.order, r.err)
	raw := Reg(uint16(0x3)) // color bits 3: not a member
	fmt.Printf("raw %v %s %d\n", raw.color, warmth(raw.color), raw.color)

	// Enums as shift counts and in volatile storage.
	var mask uint8
	for c := range Color {
		mask |= 1 << c
	}
	var vc volatile[Color]
	vc = Color.Green
	fmt.Printf("mask %b volatile %v\n", mask, vc)

	fmt.Printf("local %s %s\n", local(), local2())
}
