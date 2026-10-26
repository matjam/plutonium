package main
import "pluto/sync"

func double(in, out []int, n int) {
	var s sync.Scope
	for i := range n {
		for j := range n {
			s.Task(func() { out[i] = in[j] * 2 })
		}
	}
}

func main() { double(nil, nil, 0) }