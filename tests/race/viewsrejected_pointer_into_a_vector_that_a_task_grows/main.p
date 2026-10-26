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