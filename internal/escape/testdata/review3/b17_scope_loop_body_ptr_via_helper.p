package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
)

var got = make(chan int, 3)

func id(p *int) *int { return p }

//pu:noinline
func f() {
	var s sync.Scope
	for i := 0; i < 3; i++ {
		y := 42 + i
		p := id(&y)
		s.Task(func() {
			time.Sleep(20 * time.Millisecond)
			got <- *p
		})
	}
}

func kept() unsafe.Pointer { println("task saw", <-got, <-got, <-got); return nil }

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
