// run

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test that we can convert type parameters to both empty
// and nonempty interfaces, and named and nonnamed versions
// thereof.

package main

// Adapted from Go's test/typeparam: fmt.Sprintf is replaced by a stub, since
// Plutonium has none and the message is only seen when a check fails.
func sprintf(format string, args ...interface{}) string { return format }

type E interface{}

func f[T any](x T) interface{} {
	var i interface{} = x
	return i
}

// Plutonium: a slice converted to an interface is boxed in the converting
// function's frame, so fs and js hand the interface to use instead of
// returning it.
func fs[T any](x T, use func(interface{})) {
	y := []T{x}
	var i interface{} = y
	use(i)
}

func g[T any](x T) E {
	var i E = x
	return i
}

type C interface {
	foo() int
}

type myInt int

func (x myInt) foo() int {
	return int(x + 1)
}

func h[T C](x T) interface{ foo() int } {
	var i interface{ foo() int } = x
	return i
}
func i[T C](x T) C {
	var i C = x // conversion in assignment
	return i
}

func j[T C](t T) C {
	return C(t) // explicit conversion
}

func js[T any](x T, use func(interface{})) {
	y := []T{x}
	use(interface{}(y))
}

func main() {
	if got, want := f[int](7), 7; got != want {
		panic(sprintf("got %d want %d", got, want))
	}
	fs[int](7, func(got interface{}) {
		if want := []int{7}; got.([]int)[0] != want[0] {
			panic(sprintf("got %d want %d", got, want))
		}
	})
	if got, want := g[int](7), 7; got != want {
		panic(sprintf("got %d want %d", got, want))
	}
	if got, want := h[myInt](7).foo(), 8; got != want {
		panic(sprintf("got %d want %d", got, want))
	}
	if got, want := i[myInt](7).foo(), 8; got != want {
		panic(sprintf("got %d want %d", got, want))
	}
	if got, want := j[myInt](7).foo(), 8; got != want {
		panic(sprintf("got %d want %d", got, want))
	}
	js[int](7, func(got interface{}) {
		if want := []int{7}; got.([]int)[0] != want[0] {
			panic(sprintf("got %d want %d", got, want))
		}
	})
}
