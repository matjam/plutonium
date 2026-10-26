package main
import "pluto/sync"

func half(i int) int { return i / 2 }

func fill(out []int) {
	var s sync.Scope
	for i := range out {
		s.Task(func() { out[half(i)] = i })
	}
}

func main() { fill(nil) }