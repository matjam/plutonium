package main

import (
	"pluto/rt"
	"pluto/sync"
	"pluto/time"
)

// A func value that may be a closure of a frame must not reach a task start
// (spec sections 12.12 and 18.3): a variable assigned twice, a field, a
// parameter passed on, wg.Task, rt.Go and time.AfterFunc. Func values that
// are named functions or are loaded from package memory are fine.

type Job struct{ run func() }

func work() {}

var global func() = work

func start(f func()) {
	task f()
}

func viaHelper(f func()) {
	start(f)
}

func twice() {
	n := 0
	f := work
	f = func() { n++ }
	task f()
}

func field() {
	n := 0
	j := Job{run: func() { n++ }}
	task j.run()
}

func helper() {
	n := 0
	f := work
	f = func() { n++ }
	viaHelper(f)
}

func waitGroup() {
	n := 0
	f := work
	f = func() { n++ }
	var wg sync.WaitGroup
	wg.Task(f)
	wg.Wait()
}

func runtimeGo() {
	n := 0
	f := work
	f = func() { n++ }
	rt.Go(f, rt.MinStackSize)
}

func afterFunc() {
	n := 0
	f := work
	f = func() { n++ }
	time.AfterFunc(time.Second, f)
}

func fine() {
	var wg sync.WaitGroup
	wg.Task(work)
	wg.Task(global)
	wg.Wait()
	start(work)
	viaHelper(global)
	rt.Go(work, rt.MinStackSize)
	time.AfterFunc(time.Second, work)
}

func main() {
	twice()
	field()
	helper()
	waitGroup()
	runtimeGo()
	afterFunc()
	fine()
}
