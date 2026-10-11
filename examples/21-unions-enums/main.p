package main

import "pluto/fmt"

// Enums and unions: closed sets that the compiler checks for you.
//
// An enum is a type with a fixed list of named integer constants (language
// spec section 7.12). A union holds exactly one of a fixed list of variants,
// each with its own payload stored in place, with no allocation (section
// 7.13). Both are closed: a switch on one must handle every member or
// variant, so adding a new one turns each switch that forgot it into a
// compile error instead of a bug.

type Color enum {
	Red
	Green
	Blue
}

// The first member is the zero value, so a Color you never set is Red.
func hex(c Color) string {
	var code string
	switch c { // no default: every member must be listed
	case Red:
		code = "#ff0000"
	case Green:
		code = "#00ff00"
	case Blue:
		code = "#0000ff"
	}
	return code
}

type Shape union {
	Circle struct{ r float64 }
	Rect   struct{ w, h float64 }
	Dot    // a variant with no payload
}

// A type switch picks the variant a union holds. In a clause that names one
// variant, v is that variant's payload.
func area(s Shape) float64 {
	switch v := s.(type) {
	case Circle:
		return 3.14159 * v.r * v.r
	case Rect:
		return v.w * v.h
	case Dot:
		return 0
	}
}

// A union can be generic. This one carries either a value or a reason.
type Result[T any] union {
	Ok  T
	Err string
}

func divide(a, b int) Result[int] {
	if b == 0 {
		return Result[int].Err("divide by zero")
	}
	return Result[int].Ok(a / b)
}

func show(r Result[int]) {
	switch v := r.(type) {
	case Ok:
		fmt.Println("ok:", v)
	case Err:
		fmt.Println("error:", v)
	}
}

func main() {
	for c := range Color { // every member, in declaration order
		fmt.Println(c, hex(c))
	}
	var unset Color
	fmt.Println("zero value:", unset, "of", len(Color), "colors")

	// A non-constant integer converts to an enum only in the comma-ok form.
	for _, n := range [2]int{1, 7} {
		if c, ok := Color(n); ok {
			fmt.Println(n, "is", c)
		} else {
			fmt.Println(n, "is not a color")
		}
	}

	shapes := [3]Shape{Shape.Circle{r: 1}, Shape.Rect{w: 2, h: 3}, Shape.Dot}
	for _, s := range shapes {
		fmt.Printf("%v area %.2f\n", s, area(s))
	}

	show(divide(84, 2))
	show(divide(1, 0))
}
