package main
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	a := vec.New[int]()
	b := vec.New[int]()
	a.Push(1)
	p := a.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		for i := 0; i < 100; i++ {
			b.Push(i)
		}
	})
	println(*p)
	s.Wait()
	println(b.Len())
}