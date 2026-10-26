package main
import "pluto/sync"

func fill(out []int, k int) {
	var s sync.Scope
	for i := 0; i < len(out); i += k {
		s.Task(func() {
			w := out[i:min(i+k, len(out))]
			for j := range w {
				w[j] = i
			}
		})
	}
}

func main() { fill(nil, 2) }