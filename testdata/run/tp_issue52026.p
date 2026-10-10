// run

// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

// Plutonium: Some[T]{} converted to Option[T] is boxed in the converting
// function's frame, so withOption hands the option to f instead of
// returning it.
func withOption[T any](n int, f func(Option[T])) {
	if n == 1 {
		f(Some[T]{})
	} else {
		f(None{})
	}
}

type Option[T any] interface {
	sealedOption()
}

type Some[T any] struct {
	val T
}

func (s Some[T]) Value() T {
	return s.val
}

func (s Some[T]) sealedOption() {}

type None struct{}

func (s None) sealedOption() {}

func main() {
	withOption[int](1, func(s Option[int]) {
		_ = s.(Some[int])
	})

	withOption[int](0, func(s Option[int]) {
		_ = s.(None)

		switch (any)(s).(type) {
		case Some[int]:
			panic("s is a Some[int]")
		case None:
			// ok
		default:
			panic("oops")
		}
	})
}
