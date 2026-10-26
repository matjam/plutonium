package main
import "pluto/sync"

func add(a, b, out []int) {
	var s sync.Scope
	for i := range a {
		s.Task(func() { out[i] = a[i] + b[i] })
	}
}

func main() { add(nil, nil, nil) }