package main

import "pluto/fmt"

// A function that takes an element of what its parameter points to (&p[1])
// and passes it to a method that keeps its receiver keeps a pointer into its
// caller's variable, whatever the caller passed: the whole array, or a slice
// of it (spec section 18.3).

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

// A type of its own, whose method keeps nothing.
type Plain struct{ n int }

func (p *Plain) String() string { return "plain" }

//pu:noinline
func call(s Stringer) string { return s.String() }

//pu:noinline
func callf(f func(int) bool) bool { return f(1) }

//pu:noinline
func elem(p *[2]S) string { return call(&p[1]) }

//pu:noinline
func elemMethodValue(p *[2]S) bool { return callf(p[1].visit) }

//pu:noinline
func each(items []S) string {
	out := ""
	for i := range items {
		var s Stringer = &items[i]
		out += s.String()
	}
	return out
}

//pu:noinline
func twoDown(p *[2]S) string { return elem(p) }

type Arr [2]S

func (a *Arr) first() string { return call(&a[1]) }

//pu:noinline
func printElem(p *[2]S) { fmt.Println(&p[1]) }

//pu:noinline
func plainElem(p *[2]Plain) string { return call(&p[1]) }

func Element() {
	var arr [2]S
	println(elem(&arr))
}

func MethodValue() {
	var arr [2]S
	println(elemMethodValue(&arr))
}

func Range() {
	items := [3]S{}
	println(each(items[:]))
}

func TwoCallsDown() {
	var arr [2]S
	println(twoDown(&arr))
}

func NamedArray() {
	var arr Arr
	println(arr.first())
}

func Printed() {
	var arr [2]S
	printElem(&arr)
}

// Fine: an element of an array of another type is no receiver of (*S).String.
func Fine() {
	var arr [2]Plain
	println(plainElem(&arr))
}

func main() {
	Element()
	MethodValue()
	Range()
	TwoCallsDown()
	NamedArray()
	Printed()
	Fine()
}
