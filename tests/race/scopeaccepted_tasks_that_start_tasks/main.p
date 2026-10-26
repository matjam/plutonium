package main
import "pluto/sync"

func main() {
	var out [4]int
	var s sync.Scope
	s.Task(func() {
		for i := range out {
			s.Task(func() { out[i] = i })
		}
	})
	s.Wait()
	println(out[3])
}