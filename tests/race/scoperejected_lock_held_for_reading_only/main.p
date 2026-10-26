package main
import "pluto/sync"

func main() {
	var rw sync.RWMutex
	var n int
	var s sync.Scope
	for i := 0; i < 2; i++ {
		s.Task(func() {
			rw.RLock()
			n = i
			rw.RUnlock()
		})
	}
	s.Wait()
	println(n)
}