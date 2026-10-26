package main
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	mu.Lock()
	p := v.Ptr(0)
	println(*p)
	mu.Unlock()
	s.Wait()
}