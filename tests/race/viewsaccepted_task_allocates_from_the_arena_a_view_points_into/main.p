package main

import (
	"pluto/arena"
	"pluto/sync"
	"pluto/vec"
)

// The task allocates from the arena p points into but takes nothing back:
// by section 18.9, Alloc never hands out memory it handed out before, so
// the task's vector and p do not meet.
func main() {
	var mu sync.Mutex
	a := arena.NewFixed(1024)
	v := vec.New[int](&a)
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		w := vec.New[int](&a)
		w.Push(7)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}
