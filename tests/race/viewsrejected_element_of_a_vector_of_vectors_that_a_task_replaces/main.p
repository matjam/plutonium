package main
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[ints]()
	v.Push(vec.New[int]())
	v.Ptr(0).Push(1)
	p := v.Ptr(0).Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Set(0, vec.New[int]())
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}