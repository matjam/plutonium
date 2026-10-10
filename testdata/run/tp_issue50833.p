// run

// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

type (
	S  struct{ f int }
	PS *S
)

// Plutonium: a slice literal and the &S{...} its elements elide live in the
// frame, so the functions use them rather than return them.

func a() int { s := []*S{{f: 1}}; return s[0].f }
func b() int { s := []PS{{f: 1}}; return s[0].f }

func c[P *S]() int { s := []P{{f: 1}}; return len(s) }
func d[P PS]() int { s := []P{{f: 1}}; return len(s) }

func main() {
	c[*S]()
	d[PS]()
}
