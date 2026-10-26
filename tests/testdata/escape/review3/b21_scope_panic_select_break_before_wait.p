package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
)

var got = make(chan int, 4)
var tick = make(chan bool, 1)

//pu:noinline
func f() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		y := 42 + i
		s.Task(func() {
			time.Sleep(20 * time.Millisecond)
			got <- y
		})
		tick <- true
		select {
		case <-tick:
			if i == 0 {
				break
			}
		}
		s.Wait()
	}
}

func kept() unsafe.Pointer { println("tasks saw (42 43)", <-got, <-got); return nil }

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
