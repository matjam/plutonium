package main
import "pluto/sync"

func double(in, out []int, k int) {
	var s sync.Scope
	for i := range len(in) / k {
		s.Task(func() {
			w := in[i*k : i*k+k]
			out[i] = w[0] * 2
		})
	}
}

func main() { double(nil, nil, 1) }