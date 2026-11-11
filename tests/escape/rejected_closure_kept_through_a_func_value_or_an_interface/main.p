package main

// A closure passed through a func value or an interface to a callee that
// keeps it is kept: its captured variables are in the frame (language
// spec section 11.5, D68). Only a literal written as the argument of s.Task
// may go to a scoped task.

import "pluto/sync"

var keep func()
var keepAny any

func store(f func()) { keep = f }
func storeAny(i any) { keepAny = i }

// Through a func value.
func viaFuncValue() {
	x := 1
	f := func() { x++ }
	var g func() = f
	store(g)
}

// Through an interface.
func viaInterface() {
	x := 1
	f := func() { x++ }
	storeAny(f)
}

// Through a func value to s.Task.
func viaScopedTask() {
	x := 1
	f := func() { x++ }
	var s sync.Scope
	s.Task(f)
	s.Wait()
}

func main() {
	viaFuncValue()
	viaInterface()
	viaScopedTask()
}
