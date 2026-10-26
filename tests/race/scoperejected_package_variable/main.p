package main
import "pluto/sync"

var hits int

func main() {
	var s sync.Scope
	for i := 0; i < 2; i++ {
		s.Task(func() { hits++ })
	}
}