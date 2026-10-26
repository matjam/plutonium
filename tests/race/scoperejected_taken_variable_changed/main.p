package main
import "pluto/sync"

func run(items []int) {
	var s sync.Scope
	for _, it := range items {
		n := 0
		s.Task(func() { n = it })
		println(n)
	}
}

func main() { run(nil) }