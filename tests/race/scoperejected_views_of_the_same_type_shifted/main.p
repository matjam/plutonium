package main
import "pluto/sync"

func double(in, out []int) {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i+1] * 2 })
	}
}

func main() { double(nil, nil) }