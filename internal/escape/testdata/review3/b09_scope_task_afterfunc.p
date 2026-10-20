package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
)

var got = make(chan int, 1)

//pu:noinline
func f() {
	var s sync.Scope
	x := 42
	s.Task(func() {
		time.AfterFunc(20*time.Millisecond, func() { got <- x })
	})
}

func kept() unsafe.Pointer { println(clobber(5)); println("timer saw", <-got); return nil }

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
