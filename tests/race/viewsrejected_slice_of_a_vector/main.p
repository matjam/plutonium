package main
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	xs := v.Slice()
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(xs[0])
	s.Wait()
}