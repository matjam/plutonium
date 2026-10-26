package main
import "pluto/sync"

func main() {
	x := 0
	for i := 0; i < 3; i++ {
		var s sync.Scope
		s.Task(func() { x += i })
	}
	for i := 0; i < 3; i++ {
		var s sync.Scope
		s.Task(func() { x++ })
		s.Wait()
		x--
	}
	println(x)
}