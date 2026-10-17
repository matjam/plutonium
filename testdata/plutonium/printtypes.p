package main

// print and println write a value whose underlying type is boolean,
// integer (enums included), floating-point or string, whatever the name of
// the type (language spec section 13.12). Floats are written in the
// shortest form that reads back as the same value, as in Go 1.27.

type Flag bool

type Count int

type Small uint8

type Offset int64

type Ratio float64

type Half float32

type Label string

type Color enum {
	Red
	Green
	Blue
}

type Level enum uint8 {
	Low
	High
}

func main() {
	println(Flag(true), Flag(false), Count(-42), Small(200), Offset(1<<40), Label("héllo"))
	println(Color.Red, Color.Green, Color.Blue, Level.Low, Level.High)
	var c Color = Color.Blue
	print(c, " ", Level(1), "\n")

	println(Ratio(1.5), Ratio(100000), Ratio(1000000), Ratio(1e21), Ratio(0.000123), Ratio(0.00001))
	println(Half(0.1), Half(16777216), Half(3.4e38), 0.1, float32(0.1), 2.5e-7)
	println(1.0, 1e100, -3.25, 123456789.0)
	x := 0.1
	y := 0.2
	println(x+y, float32(x)+float32(y))
}
