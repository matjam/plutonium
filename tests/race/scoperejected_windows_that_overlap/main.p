package main
import "pluto/sync"

func fill(out []int) {
	var s sync.Scope
	for i := 0; i+3 <= len(out); i += 2 {
		s.Task(func() {
			w := out[i : i+3]
			w[0] = i
		})
	}
}

func main() { fill(nil) }