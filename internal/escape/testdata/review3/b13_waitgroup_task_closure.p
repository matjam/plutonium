package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
)

var got = make(chan int, 1)
var wg sync.WaitGroup

//pu:noinline
func f() {
	x := 42
	wg.Task(func() {
		time.Sleep(20 * time.Millisecond)
		got <- x
	})
}

func kept() unsafe.Pointer { println(clobber(3)); wg.Wait(); println("task saw", <-got); return nil }

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
