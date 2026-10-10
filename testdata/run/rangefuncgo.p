// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Range over functions with well-behaved iterators: nested loops with
// break, continue, labelled break and continue, goto and return.
//
// Adapted from Go's cmd/compile/internal/rangefunc/rangefunc_test.go: the
// tests that need recover, that run a defer once per iteration, or whose
// iterators keep yield after returning are left out; results go to slices
// with room enough, since append does not grow a slice in Plutonium; twice
// no longer recovers; Bug70035 builds numbers instead of strings.

package main

type Seq[T any] func(yield func(T) bool)
type Seq2[T1, T2 any] func(yield func(T1, T2) bool)

// OfSliceIndex returns a Seq2 over the elements of s. It is equivalent
// to range s.
func OfSliceIndex[T any, S ~[]T](s S) Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
		return
	}
}

// Values returns a Seq over the elements of s.
func Values[T any, S ~[]T](s S) Seq[T] {
	return func(yield func(T) bool) {
		for _, v := range s {
			if !yield(v) {
				return
			}
		}
	}
}

const DONE = 0      // body of loop has exited in a non-panic way
const READY = 1     // body of loop has not exited yet, is not running
const PANIC = 2     // body of loop is either currently running, or has panicked
const EXHAUSTED = 3 // iterator function return, i.e., sequence is "exhausted"

const MISSING_PANIC = 4 // overload "READY" for panic call

var fail = [...]string{"done", "ready", "panic", "exhausted", "missing panic"}

// Check2 wraps the function body passed to iterator forall
// in code that ensures that it cannot (successfully) be called
// either after body return false (control flow out of loop) or
// forall itself returns (the iteration is now done).
//
// Note that this can catch errors before the inserted checks.
func Check2[U, V any](forall Seq2[U, V]) Seq2[U, V] {
	return func(body func(U, V) bool) {
		state := READY
		forall(func(u U, v V) bool {
			tmp := state
			state = PANIC
			if tmp != READY {
				panic(fail[tmp])
			}
			ret := body(u, v)
			if ret {
				state = READY
			} else {
				state = DONE
			}
			return ret
		})
		if state == PANIC {
			panic(fail[MISSING_PANIC])
		}
		state = EXHAUSTED
	}
}

func Check[U any](forall Seq[U]) Seq[U] {
	return func(body func(U) bool) {
		state := READY
		forall(func(u U) bool {
			tmp := state
			state = PANIC
			if tmp != READY {
				panic(fail[tmp])
			}
			ret := body(u)
			if ret {
				state = READY
			} else {
				state = DONE
			}
			return ret
		})
		if state == PANIC {
			panic(fail[MISSING_PANIC])
		}
		state = EXHAUSTED
	}
}

func equal(s, t []int) bool {
	if len(s) != len(t) {
		return false
	}
	for i := range s {
		if s[i] != t[i] {
			return false
		}
	}
	return true
}

func expect(name string, want, got []int) {
	print(name, ":")
	for _, x := range got {
		print(" ", x)
	}
	println()
	if !equal(want, got) {
		panic(name)
	}
}

// TestNoVars ensures that versions of rangefunc that use zero or one
// iteration variable (instead of two) run the proper number of times
// and in the one variable case supply the proper values.
// For #65236.
func TestNoVars() {
	i, k := 0, 0
	for range Check2(OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})) {
		i++
	}
	for j := range Check2(OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})) {
		k += j
	}
	println("novars", i, k)
	if i != 10 || k != 45 {
		panic("TestNoVars")
	}
}

// TestBreak1 should just work, with well-behaved iterators.
// (The misbehaving iterator detector should not trigger.)
func TestBreak1() {
	result := make([]int, 0, 64)
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4}) {
		if x == -4 {
			break
		}
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				break
			}
			result = append(result, y)
		}
		result = append(result, x)
	}
	expect("break1", []int{1, 2, -1, 1, 2, -2, 1, 2, -3}, result)
}

// TestBreak2 should just work, with well-behaved iterators.
// (The misbehaving iterator detector should not trigger.)
func TestBreak2() {
	result := make([]int, 0, 64)
outer:
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4}) {
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				break
			}
			if x == -4 {
				break outer
			}

			result = append(result, y)
		}
		result = append(result, x)
	}
	expect("break2", []int{1, 2, -1, 1, 2, -2, 1, 2, -3}, result)
}

// TestContinue should just work, with well-behaved iterators.
// (The misbehaving iterator detector should not trigger.)
func TestContinue() {
	result := make([]int, 0, 64)
outer:
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4}) {
		result = append(result, x)
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				continue outer
			}
			if x == -4 {
				break outer
			}

			result = append(result, y)
		}
		result = append(result, x-10)
	}
	expect("continue", []int{-1, 1, 2, -2, 1, 2, -3, 1, 2, -4}, result)
}

// TestBreak3 should just work, with well-behaved iterators.
// (The misbehaving iterator detector should not trigger.)
func TestBreak3() {
	result := make([]int, 0, 64)
X:
	for _, x := range OfSliceIndex([]int{100, 200, 300, 400}) {
	Y:
		for _, y := range OfSliceIndex([]int{10, 20, 30, 40}) {
			if 10*y >= x {
				break
			}
			result = append(result, y)
			if y == 30 {
				continue X
			}
		Z:
			for _, z := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
				if z&1 == 1 {
					continue Z
				}
				result = append(result, z)
				if z >= 4 {
					continue Y
				}
			}
			result = append(result, -y) // should never be executed
		}
		result = append(result, x)
	}
	expect("break3", []int{100, 10, 2, 4, 200, 10, 2, 4, 20, 2, 4, 300, 10, 2, 4, 20, 2, 4, 30}, result)
}

// TestMultiCont0 tests multilevel continue with no bad iterators
// (it should just work)
func TestMultiCont0() {
	result := make([]int, 0, 64)

W:
	for _, w := range OfSliceIndex([]int{1000, 2000}) {
		result = append(result, w)
		if w == 2000 {
			break
		}
		for _, x := range OfSliceIndex([]int{100, 200, 300, 400}) {
			for _, y := range OfSliceIndex([]int{10, 20, 30, 40}) {
				result = append(result, y)
				for _, z := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
					if z&1 == 1 {
						continue
					}
					result = append(result, z)
					if z >= 4 {
						continue W // modified to be multilevel
					}
				}
				result = append(result, -y) // should never be executed
			}
			result = append(result, x)
		}
	}
	expect("multicont0", []int{1000, 10, 2, 4, 2000}, result)
}

// MultiBreak0 is TestMultiBreak0 with good iterators: a multilevel break.
func MultiBreak0() {
	result := make([]int, 0, 64)

W:
	for _, w := range Check2(OfSliceIndex([]int{1000, 2000})) {
		result = append(result, w)
		if w == 2000 {
			break
		}
		for _, x := range OfSliceIndex([]int{100, 200, 300, 400}) {
			for _, y := range Check2(OfSliceIndex([]int{10, 20, 30, 40})) {
				result = append(result, y)
				for _, z := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
					if z&1 == 1 {
						continue
					}
					result = append(result, z)
					if z >= 4 {
						break W
					}
				}
				result = append(result, -y) // should never be executed
			}
			result = append(result, x)
		}
	}
	expect("multibreak0", []int{1000, 10, 2, 4}, result)
}

// testReturn1 has no bad iterators.
func testReturn1(result []int) []int {
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4, -5}) {
		result = append(result, x)
		if x == -4 {
			break
		}
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				return result
			}
			result = append(result, y)
		}
		result = append(result, x)
	}
	return nil
}

// testReturn4 has no bad iterators, but exercises return variable rewriting.
func testReturn4(result []int) (_ []int, _ []int, err any) {
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4, -5}) {
		result = append(result, x)
		if x == -4 {
			break
		}
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				return result, result, nil
			}
			result = append(result, y)
		}
		result = append(result, x)
	}
	return
}

func TestReturns() {
	expect("return1", []int{-1, 1, 2}, testReturn1(make([]int, 0, 64)))
	r1, r2, err := testReturn4(make([]int, 0, 64))
	expect("return4", []int{-1, 1, 2}, r1)
	expect("return4", []int{-1, 1, 2}, r2)
	if err != nil {
		panic("testReturn4")
	}
}

// testGotoA1 tests loop-nest-internal goto, no bad iterators.
func testGotoA1(result []int) []int {
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4, -5}) {
		result = append(result, x)
		if x == -4 {
			break
		}
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				goto A
			}
			result = append(result, y)
		}
		result = append(result, x)
	A:
	}
	return result
}

// testGotoB1 tests loop-nest-exiting goto, no bad iterators.
func testGotoB1(result []int) []int {
	for _, x := range OfSliceIndex([]int{-1, -2, -3, -4, -5}) {
		result = append(result, x)
		if x == -4 {
			break
		}
		for _, y := range OfSliceIndex([]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
			if y == 3 {
				goto B
			}
			result = append(result, y)
		}
		result = append(result, x)
	}
B:
	result = append(result, 999)
	return result
}

func TestGoto() {
	expect("gotoA1", []int{-1, 1, 2, -2, 1, 2, -3, 1, 2, -4}, testGotoA1(make([]int, 0, 64)))
	expect("gotoB1", []int{-1, 1, 2, 999}, testGotoB1(make([]int, 0, 64)))
}

// twice calls yield twice.
func twice[T any](x, y T) Seq[T] {
	return func(yield func(T) bool) {
		if !yield(x) {
			return
		}
		yield(y)
	}
}

func TestTwoLevelReturn() {
	f := func() int {
		for a := range twice(0, 1) {
			for b := range twice(0, 2) {
				x := a + b
				if x == 3 {
					return x
				}
			}
		}
		return -1
	}
	y := f()
	println("twolevel", y)
	if y != 3 {
		panic("TestTwoLevelReturn")
	}
}

func TestTwoLevelReturnCheck() {
	f := func() int {
		for a := range Check(twice(0, 1)) {
			for b := range Check(twice(0, 2)) {
				x := a + b
				if x == 3 {
					return x
				}
			}
		}
		return -1
	}
	y := f()
	println("twolevelcheck", y)
	if y != 3 {
		panic("TestTwoLevelReturnCheck")
	}
}

func Bug70035(s1, s2, s3 []int) int {
	var c1 int
	for v1 := range Values(s1) {
		var c2 int
		for v2 := range Values(s2) {
			var c3 int
			for v3 := range Values(s3) {
				c3 = c3*10 + v3
			}
			c2 = c2*1000 + v2*100 + c3
		}
		c1 = c1*10 + v1 + c2
	}
	return c1
}

func Test70035() {
	got := Bug70035([]int{1, 2, 3}, []int{4, 5, 6}, []int{7, 8, 9})
	println("70035", got)
}

func main() {
	TestNoVars()
	TestBreak1()
	TestBreak2()
	TestContinue()
	TestBreak3()
	TestMultiCont0()
	MultiBreak0()
	TestReturns()
	TestGoto()
	TestTwoLevelReturn()
	TestTwoLevelReturnCheck()
	Test70035()
}
