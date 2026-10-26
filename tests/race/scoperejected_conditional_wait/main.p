package main
import "pluto/sync"

func run(fast bool) int {
	var res [2]int
	var s sync.Scope
	for i := range res {
		s.Task(func() { res[i] = i })
	}
	if fast {
		s.Wait()
	}
	res[0] = 5
	return res[1]
}

func main() { println(run(true)) }