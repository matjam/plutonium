package main

type Color enum {
	Red
	Green
	Blue
}

func paint(c Color) {}

func main() {
	var c Color = 1
	var d Color
	d = 2
	_ = c == 1
	paint(0)
	switch d {
	case 1:
	case Color.Red, Color.Green, Color.Blue:
	}
	e := Color(1) // valid: 1 is the value of a member
	var n int = "not a number"
	_, _, _ = d, e, n
}
