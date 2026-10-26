package main
import "pluto/sync"

func double(in, out []int) int {
	var s sync.Scope
	for i := range in {
		s.Task(func() { out[i] = in[i] * 2 })
		_ = out[0]
	}
	return 0
}

func main() { double(nil, nil) }