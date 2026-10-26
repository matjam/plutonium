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
		v.Push(2)
		mu.Unlock()
	})
	s.Wait()
	p = v.Ptr(0)
	println(*p)
}