package main
import (
	"pluto/sync"
	"pluto/vec"
)

type holder struct{ p *int }

func main() {
	var mu sync.Mutex
	v := vec.New[int]()
	v.Push(1)
	h := holder{p: v.Ptr(0)}
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		v.Push(2)
		mu.Unlock()
	})
	println(*h.p)
	s.Wait()
}