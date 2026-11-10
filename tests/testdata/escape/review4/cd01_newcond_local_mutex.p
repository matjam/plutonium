package main

import (
	"pluto/alloc"
	"pluto/sync"
	"unsafe"
)

var keep *int

func kept() unsafe.Pointer { return unsafe.Pointer(keep) }

// condLock is what sync.NewCond built before sync.Cond became a value
// (roadmap item 197): an object from alloc.New that keeps the lock its
// Wait releases.
type condLock struct {
	l Locker
	c sync.Cond
}

type Locker = sync.Locker

var gc *condLock

//pu:noinline
func f() {
	var mu sync.Mutex
	c := alloc.New[condLock](alloc.Default())
	c.l = &mu
	gc = c
}

//pu:noinline
func clobber(n int) int {
	var a [64]int
	for i := range a {
		a[i] = n + i
	}
	s := 0
	for i := range a {
		s += a[i]
	}
	return s
}

func main() {
	var mark int
	f()
	p := kept()
	if p == nil {
		println("nothing kept")
		return
	}
	println("dangling:", uintptr(p) < uintptr(unsafe.Pointer(&mark)))
	println(clobber(1000))
	println("value (42 if intact):", *(*int)(p))
}
