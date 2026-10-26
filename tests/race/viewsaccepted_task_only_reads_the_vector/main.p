package main
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	v := vec.New[int]()
	v.Push(1)
	p := v.Ptr(0)
	var s sync.Scope
	s.Task(func() { println(v.Len(), v.At(0)) })
	println(*p)
	s.Wait()
	v.Push(2)
}