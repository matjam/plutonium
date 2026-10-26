package main
import (
	"pluto/alloc"
	"pluto/sync"
)

type S struct{ hits int }

func main() {
	var mu sync.Mutex
	y := alloc.Synced[S]()
	q := &y.hits
	var s sync.Scope
	s.Task(func() {
		mu.Lock()
		y = nil
		mu.Unlock()
	})
	println(*q)
	s.Wait()
}