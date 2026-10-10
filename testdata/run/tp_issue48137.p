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
	return t
}

func main() {
	type Bar func() Bar
	Foo[Bar]()
}
