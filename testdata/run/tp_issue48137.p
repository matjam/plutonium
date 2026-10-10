// run

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type Constraint[T any] interface {
	~func() T
}

func Foo[T Constraint[T]]() T {
	var t T

	// Plutonium: a closure capturing t lives in this frame and could not be
	// returned, so this one captures nothing.
	t = func() T {
		var u T
		return u
	}
	// Plutonium: t's closure record is in Foo's frame, so Foo cannot
	// return it (language spec section 4.5).
	t()
	var zero T
	return zero
}

func main() {
	type Bar func() Bar
	Foo[Bar]()
}
