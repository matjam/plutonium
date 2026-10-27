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
	{
		y := 42
		p := &y
		s.Task(func() {
			time.Sleep(20 * time.Millisecond)
			got <- *p
		})
	}
	var z [16]int
	for i := range z {
		z[i] = 7
	}
	println(z[3])
}

func kept() unsafe.Pointer { println("task saw (42 if intact)", <-got); return nil }

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
