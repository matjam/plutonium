// run

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"unsafe"
)

// Adapted from Go's test/typeparam: fmt.Sprintf is replaced by a stub, since
// Plutonium has none and the message is only seen when a check fails.
func sprintf(format string, args ...interface{}) string { return format }

type S[T any] struct {
	val T
}

// Test type substitution where base type is unsafe.Pointer
type U[T any] unsafe.Pointer

func test[T any]() T {
	var q U[T]
	var v struct {
		// Test derived type that contains an unsafe.Pointer
		p   unsafe.Pointer
		val T
	}
	_ = q
	return v.val
}

func main() {
	want := 0
	got := test[int]()
	if got != want {
		panic(sprintf("got %f, want %f", got, want))
	}

}
