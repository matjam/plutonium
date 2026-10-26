package main
import "pluto/sync"

type P struct{ x, y int }

func swap(in, out []P) {
	var s sync.Scope
	for i := 0; i < len(in); i++ {
		s.Task(func() {
			out[i].x = in[i].y
			out[i].y = in[i].x
		})
	}
}

func main() { swap(nil, nil) }