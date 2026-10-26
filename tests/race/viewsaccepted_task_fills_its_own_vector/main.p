package main
import (
	"pluto/sync"
	"pluto/vec"
)

func main() {
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() {
			v := vec.New[int]()
			for j := 0; j < i; j++ {
				v.Push(j)
			}
			v = vec.New[int]()
			v.Push(i)
			println(v.Len())
		})
	}
}