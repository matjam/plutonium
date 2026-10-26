package main
import "pluto/sync"

func main() {
	var x int
	var s sync.Scope
	s.Task(func() { x = 1 })
	s.Task(func() { x = 2 })
	s.Wait()
	println(x)
}