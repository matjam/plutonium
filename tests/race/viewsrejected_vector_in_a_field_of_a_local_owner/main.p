package main
import (
	"pluto/sync"
	"pluto/vec"
)

type db owner struct {
	rows vec.Vec[int]
	n    int
}

func main() {
	var mu sync.Mutex
	var d db
	d.rows.Push(1)
	p := d.rows.Ptr(0)
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		d.rows.Push(2)
		mu.Unlock()
	})
	println(*p)
	s.Wait()
}