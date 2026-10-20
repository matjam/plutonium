package main

import "unsafe"

// A pointer in an interface may not be a pointer to a variable of the dynamic
// type's own: an element of an array, a pointer converted to another type, or
// the one pointer of a struct of one field (spec section 18.3). A method that
// keeps its receiver then keeps a pointer into the frame.

type S struct{ n int }

var keepS *S

type Stringer interface{ String() string }

func (s *S) String() string {
	keepS = s
	return "s"
}

type Inner struct{ n int }

var keepInner *Inner

type D struct{ P *Inner }

func (d D) String() string {
	keepInner = d.P
	return "d"
}

type B struct{ n int }

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func callAny(v any) string {
	if s, ok := v.(Stringer); ok {
		return s.String()
	}
	return "?"
}

// An element of an array.
func ArrayElem() {
	var arr [2]S
	println(call(&arr[1]))
}

// The same through a type assertion to the interface.
func ArrayElemAsserted() {
	var arr [2]S
	println(callAny(&arr[1]))
}

// A pointer to a variable of another type.
func Converted() {
	var b B
	println(call((*S)(&b)))
}

func ConvertedAsserted() {
	var b B
	println(callAny((*S)(&b)))
}

// A pointer made with unsafe.Pointer.
func Unsafe() {
	var x [2]int
	println(call((*S)(unsafe.Pointer(&x))))
}

// A struct of one pointer is the pointer in the interface.
func Direct() {
	var in Inner
	println(call(D{&in}))
}

func DirectVariable() {
	var in Inner
	var s Stringer = D{&in}
	println(s.String())
}

func DirectAsserted() {
	var in Inner
	println(callAny(D{&in}))
}

// Fine: a pointer to the variable of the receiver's type, with no method
// that keeps it called.
type Plain struct{ n int }

func (p *Plain) String() string { return "plain" }

func Plains() {
	var p Plain
	println(call(&p))
}

func main() {
	ArrayElem()
	ArrayElemAsserted()
	Converted()
	ConvertedAsserted()
	Unsafe()
	Direct()
	DirectVariable()
	DirectAsserted()
	Plains()
	println(keepS != nil, keepInner != nil)
}
