// Types and aliases declared in generic functions, closures inside them,
// methods of generic types and methods with their own type parameters;
// a local type embedding an interface, converted to that interface.
package main

import "unsafe"

func Alias[T any](v T) int {
	type pair = struct{ a, b T }
	p := pair{v, v}
	return int(unsafe.Sizeof(p))
}

type Box[T any] struct{ v T }

func (b Box[T]) Size() int {
	type cell struct {
		v T
		k int8
	}
	f := func() int {
		type inner [3]cell
		var x inner
		return int(unsafe.Sizeof(x))
	}
	return f()
}

func (b Box[T]) With[U any](u U) int {
	type both struct {
		t T
		u U
	}
	x := both{b.v, u}
	return int(unsafe.Sizeof(x))
}

type Shower interface{ Show() int }

type impl struct{ n int }

func (i impl) Show() int { return i.n }

func Embed[T any](v T) int {
	type wrap struct {
		Shower
		v T
	}
	w := wrap{impl{7}, v}
	var s Shower = w
	return s.Show() + int(unsafe.Sizeof(w))
}

func main() {
	// Sizes of [2]int, not of string, which is three words in Plutonium.
	println(Alias(int8(1)), Alias([2]int{}), Box[int8]{}.Size(), Box[int64]{}.Size())
	println(Box[int8]{}.With(int64(1)), Box[[2]int]{}.With(int8(1)))
	println(Embed(int8(1)), Embed([3]int{}))
}
