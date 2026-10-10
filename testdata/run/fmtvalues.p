// Composite values through fmt: structs, nested structs, slices of
// structs, arrays, pointers, interfaces and Stringers with %v, %+v, %#v,
// %T, Println and Print, compared with Go's output.

package main

import (
	"pluto/fmt"
)

type inner struct {
	X, Y int
}

type T struct {
	A int
	b string
	C []int
	D *T
	E any
	F error
	G float64
	H uint8
}

type Color int

func (c Color) String() string {
	switch c {
	case 1:
		return "red"
	case 2:
		return "green"
	}
	return "other"
}

type W struct {
	C Color
	c Color
	P [2]Color
}

type outer struct {
	In  inner
	Ins []inner
	P   *inner
	S   string
	B   bool
}

type Pair[K comparable, V any] struct {
	K K
	V V
}

type node struct {
	Val  int
	Next *node
}

type small struct {
	A int8
	B int16
	C uint32
}

type named []inner

type errT struct{ msg string }

func (e *errT) Error() string { return e.msg }

func main() {
	fmt.Printf("%+v|%#v|%v\n", 5, 5, 5)
	fmt.Printf("%#v|%#v|%#v\n", uint(5), 2.5, int8(-3))
	fmt.Printf("%v\n", T{})
	fmt.Printf("%+v\n", T{})
	fmt.Printf("%#v\n", T{})
	fmt.Printf("%#v\n", &T{A: 1, C: []int{1, 2}})
	fmt.Printf("%v %+v %#v\n", W{1, 2, [2]Color{1, 2}}, W{}, W{})

	o := outer{In: inner{1, 2}, Ins: []inner{{3, 4}, {5, 6}}, S: "s", B: true}
	fmt.Println(o)
	fmt.Printf("%v\n%+v\n%#v\n", o, o, o)
	fmt.Println(&o.In, &o.Ins, &[2]int{7, 8})
	fmt.Printf("%v %+v %#v\n", &inner{1, 2}, &inner{3, 4}, &inner{5, 6})
	fmt.Printf("%v %#v\n", named{{1, 2}}, named{})

	fmt.Println([]int{1, 2, 3}, [3]string{"a", "b", "c"}, []bool{true, false})
	fmt.Println([]int(nil), []string{}, [0]int{})
	fmt.Printf("%#v %#v %#v\n", []int(nil), []string{}, [2]bool{true})
	fmt.Printf("%v %d %x %X %s %q\n", []byte{1, 2}, []byte{1, 2}, []byte{'h', 'i'}, []byte{'h', 'i'}, []byte{'A', 'B'}, []byte{'a', '"', 'b'})
	fmt.Printf("%#v %#v %#v\n", []byte{1, 2}, []byte(nil), [2]byte{1, 2})
	fmt.Printf("%v %d %s\n", [2]byte{65, 66}, [2]byte{65, 66}, [2]byte{65, 66})
	fmt.Printf("%x %X %o\n", []int{10, 255}, [2]uint8{10, 255}, []int{8})
	fmt.Printf("%q %v\n", []string{"a", "b\n"}, [2][]int{{1}, {2, 3}})
	fmt.Printf("%#v\n", [2][]int{{1}, nil})
	fmt.Printf("%5v|%-4v|%05d|\n", []int{1, 2}, inner{1, 2}, []int{3})
	fmt.Printf("%v|%6.2f|%+v\n", []float64{1.5, 2}, []float64{1.5}, []float64{-1})

	fmt.Printf("%T %T %T %T %T\n", Pair[int, string]{}, struct{ A int }{}, []any{}, struct{}{}, &o)
	fmt.Printf("%T %T %T %T\n", []byte{}, 'x', [3]*inner{}, func(int) string { return "" })
	fmt.Printf("%#v\n", Pair[int, string]{1, "x"})
	fmt.Printf("%#v\n", struct {
		A int
		B []string
	}{1, nil})

	var np *T
	fmt.Printf("%v %+v %#v\n", np, np, np)
	fmt.Println([]*int{nil}, struct{ p *int }{})
	fmt.Printf("%#v\n", []any{1, "a", nil, 2.5, Color(1)})
	fmt.Printf("%v\n", []any{1, "a", nil, 2.5, Color(1)})
	fmt.Printf("%s|%d\n", []any{1, "a"}, T{})
	fmt.Printf("%v %v\n", small{-1, -2, 3}, struct{ e error }{})
	fmt.Printf("%v %+v\n", []error{nil, &errT{"boom"}}, struct{ Err error }{&errT{"bad"}})
	fmt.Printf("%#v\n", struct{ E error }{})

	list := &node{1, &node{2, nil}}
	fmt.Printf("%v %d\n", list.Next.Next, list.Val)
	fmt.Printf("%+v\n", *list.Next)

	fmt.Print(inner{1, 2}, inner{3, 4}, "x", []int{5}, "\n")
	fmt.Println(Color(1), []Color{1, 2, 3}, W{C: 2})
	fmt.Printf("%d %v %s\n", Color(2), Color(2), []Color{1})
	fmt.Printf("%!|%z|%d\n", T{}.A, "s")
}
