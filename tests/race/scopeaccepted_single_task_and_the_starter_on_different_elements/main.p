package main
import "pluto/sync"

func main() {
	var res [2]int
	var s sync.Scope
	s.Task(func() { res[0] = 1 })
	res[1] = 2
	s.Wait()
	println(res[0] + res[1])
}