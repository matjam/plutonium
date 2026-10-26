package main
import (
	"pluto/sync"
	"pluto/vec"
)

func first(v *vec.Vec[int]) *int { return v.Ptr(0) }

func grow(v *vec.Vec[int]) {
	for i := 0; i < 100; i++ {
		v.Push(i)
	}
}

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	p := first(&v)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		grow(&v)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}