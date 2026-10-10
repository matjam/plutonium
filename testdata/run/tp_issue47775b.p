// run

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type C[T any] struct {
}

func (c *C[T]) reset() {
}

func New[T any]() {
	c := &C[T]{}
	// Plutonium: the method value's record lives in this frame, so it is
	// kept in a local rather than the package variable i.
	j := interface{}(c.reset)
	z(j)
	z(c.reset)
}

var i interface{}

func z(interface{}) {
}

func main() {
	New[int]()
}
