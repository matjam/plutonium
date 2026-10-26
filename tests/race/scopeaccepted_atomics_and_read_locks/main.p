package main
import (
	"pluto/sync"
	"pluto/sync/atomic"
)

func main() {
	var n atomic.Int64
	var rw sync.RWMutex
	table := [4]int{1, 2, 3, 4}
	sum := 0
	var s sync.Scope
	for i := 0; i < 4; i++ {
		s.Task(func() {
			n.Add(1)
			rw.RLock()
			v := table[i]
			rw.RUnlock()
			rw.Lock()
			sum += v
			rw.Unlock()
		})
	}
	rw.Lock()
	table[0] = 9
	rw.Unlock()
	s.Wait()
	println(n.Load(), sum)
}