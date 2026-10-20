package main

import "unsafe"

// A pointer converted to another type, through unsafe.Pointer, points to
// memory of another type than its variable's. A type without methods of its
// own may hold elements or fields with methods, and a pointer to one of them
// is the receiver of those methods: the escape analysis follows the converted
// pointer as one that may be of any type (spec section 18.3).

type S struct{ n int }

var keepS *S

type Stringer interface{ String() string }

func (s *S) String() string {
	keepS = s
	return "s"
}

func (s *S) visit(x int) bool {
	keepS = s
	return x > 0
}

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func callf(f func(int) bool) bool { return f(1) }

//pu:noinline
func inCallee(p *[2]int) string {
	q := (*[2]S)(unsafe.Pointer(p))
	return call(&q[1])
}

//pu:noinline
func sum(p *int) int { return *p + 1 }

// An element of the converted array.
func Element() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	println(call(&q[1]))
}

// A method value of an element.
func MethodValue() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	println(callf(q[1].visit))
}

// The conversion in the function called.
func InCallee() {
	var x [2]int
	println(inCallee(&x))
}

// An element of a slice of the converted array.
func SliceElement() {
	var x [2]int
	q := (*[2]S)(unsafe.Pointer(&x))
	sl := q[:]
	println(call(&sl[1]))
}

// Fine: what the pointer is converted to holds nothing with methods.
func Fine() {
	var x [2]S
	q := (*[2]int)(unsafe.Pointer(&x))
	println(sum(&q[1]))
}

func main() {
	Element()
	MethodValue()
	InCallee()
	SliceElement()
	Fine()
}
