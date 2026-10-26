package main
import "pluto/sync"

func run() {
	var total int
	defer func() { println(total) }()
	var mu sync.Mutex
	var s sync.Scope
	for i := 0; i < 3; i++ {
		s.Task(func() {
			mu.Lock()
			total += i
			mu.Unlock()
		})
	}
}

func main() { run() }