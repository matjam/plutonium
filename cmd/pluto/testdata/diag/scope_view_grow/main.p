// An unsynchronized free and use, which section 4.1 of the memory model
// says safe code cannot express: a scoped task grows a vector (freeing its
// buffer) under a lock while the starting function reads through a pointer
// into the old buffer without the lock. The race check must reject it.
package main

import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		for i := 0; i < 1000; i++ {
			v.Push(i)
		}
		mu.Unlock()
	})
	sum := 0
	for i := 0; i < 1000000; i++ {
		sum += *p
	}
	s.Wait()
	println(sum)
}
