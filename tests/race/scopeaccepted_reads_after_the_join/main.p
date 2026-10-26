package main
import "pluto/sync"

func main() {
	var res [3]int
	{
		var s sync.Scope
		for i := range res {
			s.Task(func() { res[i] = i })
		}
	}
	println(res[0])
	var s2 sync.Scope
	for i := range res {
		s2.Task(func() { res[i]++ })
	}
	s2.Wait()
	println(res[1])
}