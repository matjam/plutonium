package main

// A closure converted to a named func type with methods keeps the methods of
// that type: HF.String stores its receiver, the closure record in the caller's
// frame, in a package variable (spec sections 11.5 and 18.3).

type Stringer interface{ String() string }

type HF func() int

var keepF HF

func (h HF) String() string {
	keepF = h
	return "hf"
}

type C struct{ n int }

func (c *C) get() int { return c.n }

//pu:noinline
func call(s Stringer) string { return s.String() }

// A conversion of a literal.
func Literal() {
	x := 42
	println(call(HF(func() int { return x })))
}

// Through an empty interface and an assertion.
func Asserted() {
	x := 42
	var v any = HF(func() int { return x })
	if s, ok := v.(Stringer); ok {
		println(call(s))
	}
}

// A method value, which holds a pointer to c.
func MethodValue() {
	var c C
	println(call(HF(c.get)))
}

// An assignment converts the literal to HF.
func Variable() {
	x := 42
	var h HF = func() int { return x }
	println(call(h))
}

// Fine: the method is called on the value, which keeps nothing of the frame
// when the literal captures nothing.
func Static() {
	println(call(HF(func() int { return 1 })))
}

func main() {
	Literal()
	Asserted()
	MethodValue()
	Variable()
	Static()
	println(keepF())
}
