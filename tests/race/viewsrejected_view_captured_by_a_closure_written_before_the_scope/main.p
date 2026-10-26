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
	get := func() int { return *p }
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(get())
	s.Wait()
}