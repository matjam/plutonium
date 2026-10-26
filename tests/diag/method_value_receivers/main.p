package main

import "unsafe"

// A method value holds its pointer receiver. A pointer to an element of an
// array, to a variable of another named type, or made with unsafe.Pointer is
// not a pointer to a variable of the receiver's type, whatever the type of the
// variable it points into: each of these frame pointers reaches the package
// variable (spec section 18.3).

type S struct{ n int }

var keepS *S

func (s *S) visit(x int) bool {
	keepS = s
	return x > 0
}

type B struct{ n int }

type O struct {
	S
	m int
}

type N struct {
	s S
	m int
}

//pu:noinline
func call(f func(int) bool) bool { return f(1) }

// The variable's own method.
func Local() {
	var s S
	_ = call(s.visit)
}

// An element of an array.
func ArrayElem() {
	var arr [2]S
	_ = call(arr[1].visit)
}

// An element of a slice of an array.
func SliceElem() {
	var arr [2]S
	sl := arr[:]
	_ = call(sl[1].visit)
}

// A pointer converted to another named type with the same underlying type.
func Converted() {
	var b B
	_ = call((*S)(&b).visit)
}

// A pointer made with unsafe.Pointer.
func Unsafe() {
	var x [2]int
	_ = call((*S)(unsafe.Pointer(&x)).visit)
}

// The method value is kept in a variable first.
func Stored() {
	var arr [2]S
	g := arr[0].visit
	_ = call(g)
}

// A promoted method of an embedded field, and a field.
func Fields() {
	var o O
	_ = call(o.visit)
	var n N
	_ = call(n.s.visit)
	var arr [2]N
	_ = call(arr[1].s.visit)
}

// Fine: no method value keeps its receiver.
func Static() {
	total := 0
	_ = call(func(x int) bool {
		total += x
		return true
	})
	println(total)
}

func main() {
	Local()
	ArrayElem()
	SliceElem()
	Converted()
	Unsafe()
	Stored()
	Fields()
	Static()
	println(keepS != nil)
}
