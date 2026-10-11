// run

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import "pluto/alloc"

func main() {
	Gooer2[byte]()
}

type Fooer[T any] interface {
	Foo(p T)
}

type fooer1[T any] struct{}

func (fooer1[T]) Foo(T) {}

type fooer2[T any] struct {
	r []Fooer[T]
}

//pu:noinline
func (mr fooer2[T]) Foo(p T) {
	mr.r[0] = fooer1[T]{}
	return
}

func Gooer2[T any]() Fooer[T] {
	// Plutonium: fooer2[T]{} converted to Fooer[T] would be boxed in this
	// frame and could not be returned; a pointer to an allocation can. It
	// comes from alloc.New, the unchecked layer, and is never freed: new
	// would return an owned[fooer2[T]], which the ordinary interface could
	// only borrow.
	return alloc.New[fooer2[T]]()
}
