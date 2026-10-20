package main

import (
	"unsafe"
	"pluto/sync"
	"pluto/time"
	"pluto/errors"
)

var got = make(chan int, 4)

//pu:noinline
func h(i int) (int, error) {
	if i == 0 {
		return 0, errors.New("skip")
	}
	return i, nil
}

//pu:noinline
func f() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		y := 42 + i
		s.Task(func() {
			time.Sleep(20 * time.Millisecond)
			got <- y
		})
		_ = h(i) or continue
		s.Wait()
	}
}

func kept() unsafe.Pointer { println("tasks saw (42 43 if each has its own y)", <-got, <-got); return nil }

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
