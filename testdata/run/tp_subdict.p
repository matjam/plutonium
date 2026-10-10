// run

// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test cases where a main dictionary is needed inside a generic function/method, because
// we are calling a method on a fully-instantiated type or a fully-instantiated function.
// (probably not common situations, of course)

package main

// Adapted from Go's test/typeparam: fmt.Sprintf is replaced by a stub, since
// Plutonium has none and the message is only seen when a check fails.
func sprintf(format string, args ...interface{}) string { return format }

type C comparable

type value[T C] struct {
	val T
}

func (v *value[T]) test(def T) bool {
	return (v.val == def)
}

func (v *value[T]) get(def T) T {
	var c value[int]
	if c.test(32) {
		return def
	} else if v.test(def) {
		return def
	} else {
		return v.val
	}
}

func main() {
	var s value[string]
	if got, want := s.get("ab"), ""; got != want {
		panic(sprintf("get() == %d, want %d", got, want))
	}
}
