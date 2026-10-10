// run

// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Test the 'for range' construct ranging over functions.
//
// Adapted from Go's test/range4.go. Its func7, func8 and func9 run a defer
// statement once per iteration, which Plutonium rejects as it rejects defer
// in any loop (rangefuncdefer.p tests the defers it accepts), and append
// beyond capacity; they are left out.

package main

var gj int

func yield4x(yield func() bool) {
	_ = yield() && yield() && yield() && yield()
}

func yield4(yield func(int) bool) {
	_ = yield(1) && yield(2) && yield(3) && yield(4)
}

func yield3(yield func(int) bool) {
	_ = yield(1) && yield(2) && yield(3)
}

func yield2(yield func(int) bool) {
	_ = yield(1) && yield(2)
}

func testfunc0() {
	j := 0
	for range yield4x {
		j++
	}
	if j != 4 {
		println("wrong count ranging over yield4x:", j)
		panic("testfunc0")
	}

	j = 0
	for _ = range yield4 {
		j++
	}
	if j != 4 {
		println("wrong count ranging over yield4:", j)
		panic("testfunc0")
	}
}

func testfunc1() {
	bad := false
	j := 1
	for i := range yield4 {
		if i != j {
			println("range var", i, "want", j)
			bad = true
		}
		j++
	}
	if j != 5 {
		println("wrong count ranging over f:", j)
		bad = true
	}
	if bad {
		panic("testfunc1")
	}
}

func testfunc2() {
	bad := false
	j := 1
	var i int
	for i = range yield4 {
		if i != j {
			println("range var", i, "want", j)
			bad = true
		}
		j++
	}
	if j != 5 {
		println("wrong count ranging over f:", j)
		bad = true
	}
	if i != 4 {
		println("wrong final i ranging over f:", i)
		bad = true
	}
	if bad {
		panic("testfunc2")
	}
}

func testfunc3() {
	bad := false
	j := 1
	var i int
	for i = range yield4 {
		if i != j {
			println("range var", i, "want", j)
			bad = true
		}
		j++
		if i == 2 {
			break
		}
		continue
	}
	if j != 3 {
		println("wrong count ranging over f:", j)
		bad = true
	}
	if i != 2 {
		println("wrong final i ranging over f:", i)
		bad = true
	}
	if bad {
		panic("testfunc3")
	}
}

func testfunc4() {
	bad := false
	j := 1
	var i int
	func() {
		for i = range yield4 {
			if i != j {
				println("range var", i, "want", j)
				bad = true
			}
			j++
			if i == 2 {
				return
			}
		}
	}()
	if j != 3 {
		println("wrong count ranging over f:", j)
		bad = true
	}
	if i != 2 {
		println("wrong final i ranging over f:", i)
		bad = true
	}
	if bad {
		panic("testfunc3")
	}
}

func func5() (int, int) {
	for i := range yield4 {
		return 10, i
	}
	panic("still here")
}

func testfunc5() {
	x, y := func5()
	if x != 10 || y != 1 {
		println("wrong results", x, y, "want", 10, 1)
		panic("testfunc5")
	}
}

func func6() (z, w int) {
	for i := range yield4 {
		z = 10
		w = i
		return
	}
	panic("still here")
}

func testfunc6() {
	x, y := func6()
	if x != 10 || y != 1 {
		println("wrong results", x, y, "want", 10, 1)
		panic("testfunc6")
	}
}

// test that range evaluates the index and value expressions
// exactly once per iteration.

var ncalls = 0

func getvar(p *int) *int {
	ncalls++
	return p
}

func iter2(list ...int) func(func(int, int) bool) {
	return func(yield func(int, int) bool) {
		for i, x := range list {
			if !yield(i, x) {
				return
			}
		}
	}
}

func testcalls() {
	var i, v int
	ncalls = 0
	si := 0
	sv := 0
	for *getvar(&i), *getvar(&v) = range iter2(1, 2) {
		si += i
		sv += v
	}
	if ncalls != 4 {
		println("wrong number of calls:", ncalls, "!= 4")
		panic("fail")
	}
	if si != 1 || sv != 3 {
		println("wrong sum in testcalls", si, sv)
		panic("fail")
	}
}

type iter3YieldFunc func(int, int) bool

func iter3(list ...int) func(iter3YieldFunc) {
	return func(yield iter3YieldFunc) {
		for k, v := range list {
			if !yield(k, v) {
				return
			}
		}
	}
}

func testcalls1() {
	ncalls := 0
	for k, v := range iter3(1, 2, 3) {
		_, _ = k, v
		ncalls++
	}
	if ncalls != 3 {
		println("wrong number of calls:", ncalls, "!= 3")
		panic("fail")
	}
}

func main() {
	testfunc0()
	testfunc1()
	testfunc2()
	testfunc3()
	testfunc4()
	testfunc5()
	testfunc6()
	testcalls()
	testcalls1()
	println("ok")
}
