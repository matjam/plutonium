// Types declared inside functions as type arguments, where types of the
// same name in different functions must stay distinct, and types declared
// inside generic functions, which depend on the type parameters.
package main

import "unsafe"

func Bytes[T any]() int {
	var z T
	return int(unsafe.Sizeof(z))
}

func c() int {
	type T int32
	return Bytes[T]()
}

func d() int {
	type T string
	return Bytes[T]()
}

func Size[T any]() int {
	var z [2]T
	return len(Describe(z))
}

func Describe[T any](x T) string {
	switch any(x).(type) {
	case [2]int:
		return "int2"
	}
	return "other"
}

func Show[T interface{ Get() int }](x T) int { return x.Get() }

func a() int {
	type T int
	type U struct{ v int }
	return Size[T]() + Show(G1{}) + len(Name[U]())
}

func b() int {
	type T string
	type U struct{ s string }
	return Size[T]() + Show(G2{}) + len(Name[U]())
}

type G1 struct{}

func (G1) Get() int { return 1 }

type G2 struct{}

func (G2) Get() int { return 2 }

func Name[T any]() string {
	var z T
	switch any(z).(type) {
	case struct{ v int }:
		return "v"
	}
	return "other"
}

func local1() int {
	type P struct{ a, b int }
	return Sum2(P{1, 2}, func(p P) int { return p.a + p.b })
}

func local2() int {
	type P struct{ a, b, c int }
	return Sum2(P{1, 2, 3}, func(p P) int { return p.a + p.b + p.c })
}

func Sum2[T any](x T, f func(T) int) int {
	y := x
	return f(y) * 10
}

// A type declared inside a generic function depends on its type parameter.
func Inner[T any](v T) int {
	type box struct {
		v T
		n int
	}
	b := box{v, 1}
	return Count(b) + b.n
}

func Count[T any](x T) int {
	var arr [3]T
	arr[1] = x
	_ = arr
	return 3
}

// A recursive type declared in a generic function.
func Rec[T any](v T) int {
	type node struct {
		v    T
		next *node
	}
	n2 := node{v: v}
	n1 := node{v: v, next: &n2}
	k := 0
	for p := &n1; p != nil; p = p.next {
		k++
	}
	return k*100 + Bytes[node]()
}

func Wrap[T any](v T) any {
	type wrapped struct{ v T }
	return wrapped{v}
}

func main() {
	println(a(), b(), local1(), local2(), Inner(1), Inner("s"), c(), d())
	println(Bytes[struct{ a, b int }](), Rec(3), Rec("x"))
	w1, w2 := Wrap(1), Wrap("x")
	_, ok := w1.(interface{})
	println(ok, w1 == Wrap(1), w1 == w2)
}
