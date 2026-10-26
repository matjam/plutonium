package main
import (
	"pluto/alloc"
	"pluto/sync"
)

func main() {
	var mu sync.Mutex
	x := alloc.New[int]()
	*x = 1
	p := x
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		alloc.Free(x)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}