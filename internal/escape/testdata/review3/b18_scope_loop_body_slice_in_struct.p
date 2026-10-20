package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
)

type H struct{ s []int }

var got = make(chan int, 3)

//pu:noinline
func f() {
	var s sync.Scope
	for i := 0; i < 3; i++ {
		arr := [2]int{42 + i, 0}
		h := H{arr[:]}
		s.Task(func() {
			time.Sleep(20 * time.Millisecond)
			got <- h.s[0]
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
