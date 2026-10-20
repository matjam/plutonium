package main

import "pluto/fmt"

// fmt walks the values it prints and calls the String and Error methods of
// the values inside them: of struct fields, slice and array elements,
// pointers and union payloads. A method that keeps a pointer it reaches
// keeps a pointer to the frame of the function that printed the value
// (spec section 18.3).

type Inner struct{ n int }

var keep *Inner

// A pointer method that keeps its receiver.
func (i *Inner) String() string {
	keep = i
	return "inner"
}

// A value method that keeps a pointer the value holds.
type W struct {
	P   *Inner
	pad int
}

func (w W) String() string {
	keep = w.P
	return "w"
}

// An error whose value holds a pointer.
type E struct {
	P   *Inner
	pad int
}

func (e E) Error() string {
	keep = e.P
	return "e"
}

// A struct of one pointer is the pointer in an interface.
type D struct{ P *Inner }

func (d D) String() string {
	keep = d.P
	return "d"
}

type Outer struct {
	W W
}

type WithPtr struct {
	P *Inner
}

type WithDirect struct {
	D D
}

type Choice union {
	None
	P *Inner
	V W
}

// The value is the operand.
func Operand() {
	var in Inner
	fmt.Println(&in)
}

// The method is of a field.
func Field() {
	var in Inner
	fmt.Println(Outer{W{&in, 1}})
}

// The method is of what a field points to.
func FieldPointer() {
	var in Inner
	fmt.Println(WithPtr{&in})
}

// The method is of an element of a slice.
func SliceElem() {
	var in Inner
	fmt.Println([]*Inner{&in})
}

// And of an array.
func ArrayElem() {
	var in Inner
	fmt.Println([1]W{{&in, 1}})
}

// A pointer-shaped struct, as a field and as the operand.
func Direct() {
	var in Inner
	fmt.Println(WithDirect{D{&in}})
}

func DirectOperand() {
	var in Inner
	fmt.Println(D{&in})
}

// An error inside a value.
func Errors() {
	var in Inner
	fmt.Println([]error{E{&in, 1}})
}

func Sprintf() {
	var in Inner
	_ = fmt.Sprintf("%v", &in)
}

// A union's payload.
func UnionPointer() {
	var in Inner
	fmt.Println(Choice.P(&in))
}

func UnionValue() {
	var in Inner
	fmt.Println(Choice.V(W{&in, 1}))
}

// Fine: the values printed hold nothing the methods keep.
func Fine() {
	fmt.Println(1, "x", 2.5, []int{1, 2}, WithPtr{nil}, Outer{W{nil, 0}})
}

func main() {
	Operand()
	Field()
	FieldPointer()
	SliceElem()
	ArrayElem()
	Direct()
	DirectOperand()
	Errors()
	Sprintf()
	UnionPointer()
	UnionValue()
	Fine()
	println(keep != nil)
}
