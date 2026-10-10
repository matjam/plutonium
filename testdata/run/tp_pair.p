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

type pair[F1, F2 any] struct {
	f1 F1
	f2 F2
}

func main() {
	p := pair[int32, int64]{1, 2}
	if got, want := unsafe.Sizeof(p.f1), uintptr(4); got != want {
		panic(sprintf("unexpected f1 size == %d, want %d", got, want))
	}
	if got, want := unsafe.Sizeof(p.f2), uintptr(8); got != want {
		panic(sprintf("unexpected f2 size == %d, want %d", got, want))
	}

	type mypair struct {
		f1 int32
		f2 int64
	}
	mp := mypair(p)
	if mp.f1 != 1 || mp.f2 != 2 {
		panic(sprintf("mp == %#v, want %#v", mp, mypair{1, 2}))
	}
}
